package browser

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// InterceptRule defines a single URL pattern to intercept and how to handle it.
type InterceptRule struct {
	// URLPattern is a substring match against the request URL.
	// Example: "/x/web-interface/search/type" matches Bilibili search API.
	URLPattern string

	// ID is a unique identifier for this rule, used to retrieve the captured response.
	// Example: "search", "user_info", "user_stat"
	ID string
}

// InterceptResult holds the captured response for a matched rule.
type InterceptResult struct {
	ID   string // matches InterceptRule.ID
	Body []byte // raw response body (JSON)
	URL  string // full request URL (for debugging)
}

// defaultInterceptTimeout is the maximum time to wait for all intercept rules to match.
// This prevents infinite blocking when the target API is not triggered (e.g., captcha page,
// page load failure, JS execution error).
const defaultInterceptTimeout = 30 * time.Second

// debugNetworkLog controls whether all network response URLs are logged to the debug log file.
// Enable this to diagnose why expected API calls are not being triggered.
// Debug messages go to the file logger (see logger.go), not stdout.
var debugNetworkLog = true

// NavigateAndIntercept opens a URL in the given Page and waits for all specified
// rules to be matched (or context timeout/cancellation).
//
// Flow:
//  1. Enable network domain and set up event listener
//  2. Navigate the Page to targetURL
//  3. Wait until all rules have captured a response, or ctx/timeout expires
//  4. Return captured results
//
// If the caller's ctx has no deadline, a default timeout of 30s is applied.
func NavigateAndIntercept(ctx context.Context, page *rod.Page, targetURL string, rules []InterceptRule) ([]InterceptResult, error) {
	if len(rules) == 0 {
		return nil, fmt.Errorf("no intercept rules provided")
	}

	// Apply default timeout if the caller's context has no deadline.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultInterceptTimeout)
		defer cancel()
	}

	// Enable network domain to receive events.
	_ = proto.NetworkEnable{}.Call(page)

	var mu sync.Mutex
	results := make(map[string]InterceptResult)

	// Set up event listener BEFORE navigating.
	// EachEvent returns a wait function that blocks until the callback returns true.
	wait := page.EachEvent(func(e *proto.NetworkResponseReceived) bool {
		// Debug: log every network response to the file logger (not stdout).
		if debugNetworkLog {
			logDebug("DEBUG: [browser] network response: status=%d type=%s url=%s",
				e.Response.Status, string(e.Type), e.Response.URL)
		}

		for _, rule := range rules {
			if strings.Contains(e.Response.URL, rule.URLPattern) {
				// Only capture 2xx responses. Non-2xx (e.g. 412 risk control) returns
				// HTML error pages that would fail JSON parsing downstream.
				if e.Response.Status < 200 || e.Response.Status >= 300 {
					log.Printf("WARN: [browser] skipping non-2xx response for %s (status=%d, url=%s)", rule.ID, e.Response.Status, e.Response.URL)
					continue
				}

				mu.Lock()
				// Only capture the first match for each rule.
				if _, exists := results[rule.ID]; !exists {
					body, err := getResponseBody(page, e.RequestID)
					if err != nil {
						log.Printf("WARN: [browser] failed to get response body for %s (url=%s): %v", rule.ID, e.Response.URL, err)
						mu.Unlock()
						continue
					}
					results[rule.ID] = InterceptResult{
						ID:   rule.ID,
						Body: body,
						URL:  e.Response.URL,
					}
					// Log a preview of the response body for debugging.
					preview := string(body)
					if len(preview) > 300 {
						preview = preview[:300] + "..."
					}
					log.Printf("INFO: [browser] intercepted %s (%d bytes): %s", rule.ID, len(body), e.Response.URL)
					log.Printf("DEBUG: [browser] %s body preview: %s", rule.ID, preview)

					if len(results) == len(rules) {
						mu.Unlock()
						return true // all rules matched, stop listening
					}
				}
				mu.Unlock()
			}
		}
		return false // continue listening
	})

	// Navigate and wait for intercept — all in a goroutine so ctx timeout covers everything.
	log.Printf("INFO: [browser] navigating to %s", targetURL)

	doneCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		if err := page.Navigate(targetURL); err != nil {
			errCh <- fmt.Errorf("navigate to %s: %w", targetURL, err)
			return
		}
		log.Printf("INFO: [browser] navigate done, waiting for API intercept: %s", targetURL)

		// Wait for all intercept rules to match.
		wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// All rules matched.
		log.Printf("INFO: [browser] all intercept rules matched for %s", targetURL)
	case err := <-errCh:
		// Navigate or other fatal error in the goroutine.
		return nil, err
	case <-ctx.Done():
		// Collect partial results for debugging.
		mu.Lock()
		matched := len(results)
		mu.Unlock()
		return nil, fmt.Errorf("intercept timeout for %s (matched %d/%d rules): %w", targetURL, matched, len(rules), ctx.Err())
	}

	// Collect results in rule order.
	mu.Lock()
	defer mu.Unlock()

	out := make([]InterceptResult, 0, len(rules))
	for _, rule := range rules {
		if r, ok := results[rule.ID]; ok {
			out = append(out, r)
		}
	}

	return out, nil
}

// WaitForIntercept sets up a listener for API responses matching the given rules
// on an already-loaded page. Used for pagination scenarios where the page is
// already open and we trigger a new API call via page interaction.
//
// Returns a wait function and a results collector. The caller should:
//  1. Call WaitForIntercept to set up the listener
//  2. Perform page interaction (click, scroll, navigate)
//  3. Call the returned wait function (blocks until all rules match or ctx expires)
//  4. Read results from the returned slice
func WaitForIntercept(ctx context.Context, page *rod.Page, rules []InterceptRule) (waitFn func() ([]InterceptResult, error)) {
	if len(rules) == 0 {
		return func() ([]InterceptResult, error) {
			return nil, fmt.Errorf("no intercept rules provided")
		}
	}

	// Enable network domain to receive events.
	_ = proto.NetworkEnable{}.Call(page)

	var mu sync.Mutex
	results := make(map[string]InterceptResult)

	// Set up event listener.
	wait := page.EachEvent(func(e *proto.NetworkResponseReceived) bool {
		for _, rule := range rules {
			if strings.Contains(e.Response.URL, rule.URLPattern) {
				// Only capture 2xx responses. Non-2xx (e.g. 412 risk control) returns
				// HTML error pages that would fail JSON parsing downstream.
				if e.Response.Status < 200 || e.Response.Status >= 300 {
					log.Printf("WARN: [browser] skipping non-2xx response for %s (status=%d, url=%s)", rule.ID, e.Response.Status, e.Response.URL)
					continue
				}

				mu.Lock()
				if _, exists := results[rule.ID]; !exists {
					body, err := getResponseBody(page, e.RequestID)
					if err != nil {
						log.Printf("WARN: [browser] failed to get response body for %s: %v", rule.ID, err)
						mu.Unlock()
						continue
					}
					results[rule.ID] = InterceptResult{
						ID:   rule.ID,
						Body: body,
						URL:  e.Response.URL,
					}
					log.Printf("INFO: [browser] WaitForIntercept matched %s (%d bytes): %s", rule.ID, len(body), e.Response.URL)

					if len(results) == len(rules) {
						mu.Unlock()
						return true // all matched
					}
				}
				mu.Unlock()
			}
		}
		return false
	})

	return func() ([]InterceptResult, error) {
		doneCh := make(chan struct{})
		go func() {
			wait()
			close(doneCh)
		}()

		select {
		case <-doneCh:
			// All rules matched.
		case <-ctx.Done():
			return nil, fmt.Errorf("intercept wait timeout: %w", ctx.Err())
		}

		mu.Lock()
		defer mu.Unlock()

		out := make([]InterceptResult, 0, len(rules))
		for _, rule := range rules {
			if r, ok := results[rule.ID]; ok {
				out = append(out, r)
			}
		}
		return out, nil
	}
}

// getResponseBody retrieves the response body for a given request ID via CDP.
// Retries a few times with short delays, because the response body may not be
// immediately available when the NetworkResponseReceived event fires.
func getResponseBody(page *rod.Page, requestID proto.NetworkRequestID) ([]byte, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		req := proto.NetworkGetResponseBody{RequestID: requestID}
		result, err := req.Call(page)
		if err == nil {
			return []byte(result.Body), nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("get response body: %w", lastErr)
}
