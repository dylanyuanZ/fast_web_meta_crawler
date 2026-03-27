package httpclient

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"time"

	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
)

// Client wraps http.Client with automatic retry and exponential backoff.
type Client struct {
	inner         *http.Client
	maxRetries    int
	initialDelay  time.Duration
	maxDelay      time.Duration
	backoffFactor float64
	cookie        string
}

// userAgent is the common User-Agent string used for all requests.
const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

// New creates a new Client using the global configuration.
// It initializes a cookie jar and warms up by visiting bilibili.com to obtain
// necessary cookies (e.g. buvid3) that are required by Bilibili APIs.
func New() *Client {
	cfg := config.Get()

	// HTTP sub-config was removed from Config; use hardcoded defaults for deprecated code.
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	c := &Client{
		inner:         httpClient,
		maxRetries:    3,
		initialDelay:  1 * time.Second,
		maxDelay:      30 * time.Second,
		backoffFactor: 2.0,
		cookie:        cfg.Cookie,
	}

	if cfg.Cookie != "" {
		// Manual cookie configured — skip cookie jar to avoid conflicts between
		// manually set Cookie header and jar-managed cookies.
		log.Printf("INFO: Using manually configured cookie (len=%d), cookie jar disabled", len(cfg.Cookie))
	} else {
		// No manual cookie — use cookie jar + warm-up to auto-obtain buvid3 etc.
		jar, _ := cookiejar.New(nil)
		httpClient.Jar = jar
		c.warmUp()
	}

	return c
}

// warmUp visits bilibili.com homepage to obtain initial cookies from Set-Cookie headers.
// This is required because Bilibili APIs return 412 without valid cookies.
func (c *Client) warmUp() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.bilibili.com", nil)
	if err != nil {
		log.Printf("WARN: cookie warm-up: failed to create request: %v", err)
		return
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.inner.Do(req)
	if err != nil {
		log.Printf("WARN: cookie warm-up: request failed: %v", err)
		return
	}
	resp.Body.Close()

	log.Printf("INFO: Cookie warm-up completed (status=%d, cookies=%d)",
		resp.StatusCode, len(c.inner.Jar.Cookies(req.URL)))
}

// Get performs an HTTP GET request with automatic retry on transient errors.
// Retries on 5xx, 429, and timeout errors. Does not retry on 4xx (except 429).
// Returns the response body bytes on success.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	delay := c.initialDelay

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("WARN: HTTP retry %d/%d, url=%s, next_wait=%v", attempt, c.maxRetries, url, delay)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			delay = c.nextDelay(delay)
		}

		body, err := c.doRequest(ctx, url)
		if err == nil {
			return body, nil
		}

		// Check if the error is retryable.
		if !isRetryable(err) {
			return nil, err
		}

		// Last attempt failed — will exit loop.
		if attempt == c.maxRetries {
			log.Printf("ERROR: HTTP all retries exhausted, url=%s, err=%v", url, err)
			return nil, err
		}
	}

	// Unreachable, but satisfies the compiler.
	return nil, fmt.Errorf("unexpected: retry loop exited without result")
}

// doRequest performs a single HTTP GET and returns the body or an error.
func (c *Client) doRequest(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &nonRetryableError{err: err}
	}

	// Set headers to mimic a real browser request.
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://www.bilibili.com/")
	req.Header.Set("Origin", "https://www.bilibili.com")
	if c.cookie != "" {
		req.Header.Set("Cookie", c.cookie)
	}

	resp, err := c.inner.Do(req)
	if err != nil {
		// Network/timeout errors are retryable.
		return nil, &retryableError{err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &retryableError{err: fmt.Errorf("read body: %w", err)}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return body, nil
	}

	statusErr := fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)

	// 429 Too Many Requests — retryable.
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &retryableError{err: statusErr}
	}

	// 412 Precondition Failed — Bilibili anti-crawl response, retryable.
	if resp.StatusCode == http.StatusPreconditionFailed {
		return nil, &retryableError{err: statusErr}
	}

	// 5xx Server Error — retryable.
	if resp.StatusCode >= 500 {
		return nil, &retryableError{err: statusErr}
	}

	// 4xx Client Error (except 429/412) — not retryable.
	return nil, &nonRetryableError{err: statusErr}
}

// nextDelay calculates the next retry delay with exponential backoff, capped at maxDelay.
func (c *Client) nextDelay(current time.Duration) time.Duration {
	next := time.Duration(float64(current) * c.backoffFactor)
	if next > c.maxDelay {
		return c.maxDelay
	}
	return next
}

// retryableError indicates a transient error that should be retried.
type retryableError struct {
	err error
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// nonRetryableError indicates a permanent error that should not be retried.
type nonRetryableError struct {
	err error
}

func (e *nonRetryableError) Error() string { return e.err.Error() }
func (e *nonRetryableError) Unwrap() error { return e.err }

// isRetryable checks if an error is retryable.
func isRetryable(err error) bool {
	_, ok := err.(*retryableError)
	return ok
}
