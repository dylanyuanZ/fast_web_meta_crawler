package browser

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

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

// NavigateAndIntercept opens a URL in the given Page and waits for all specified
// rules to be matched (or context timeout/cancellation).
//
// Flow:
//  1. Enable network domain and set up event listener
//  2. Navigate the Page to targetURL
//  3. Wait until all rules have captured a response, or ctx expires
//  4. Return captured results
func NavigateAndIntercept(ctx context.Context, page *rod.Page, targetURL string, rules []InterceptRule) ([]InterceptResult, error) {
	if len(rules) == 0 {
		return nil, fmt.Errorf("no intercept rules provided")
	}

	// Enable network domain to receive events.
	_ = proto.NetworkEnable{}.Call(page)

	var mu sync.Mutex
	results := make(map[string]InterceptResult)

	// Set up event listener BEFORE navigating.
	// EachEvent returns a wait function that blocks until the callback returns true.
	wait := page.EachEvent(func(e *proto.NetworkResponseReceived) bool {
		for _, rule := range rules {
			if strings.Contains(e.Response.URL, rule.URLPattern) {
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
					log.Printf("INFO: [browser] intercepted %s: %s", rule.ID, e.Response.URL)

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

	// Navigate to the target URL.
	if err := page.Navigate(targetURL); err != nil {
		return nil, fmt.Errorf("navigate to %s: %w", targetURL, err)
	}

	// Wait for all rules to match in a separate goroutine so we can respect ctx.
	doneCh := make(chan struct{})
	go func() {
		wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// All rules matched.
	case <-ctx.Done():
		return nil, fmt.Errorf("intercept timeout for %s: %w", targetURL, ctx.Err())
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
func getResponseBody(page *rod.Page, requestID proto.NetworkRequestID) ([]byte, error) {
	req := proto.NetworkGetResponseBody{RequestID: requestID}
	result, err := req.Call(page)
	if err != nil {
		return nil, fmt.Errorf("get response body: %w", err)
	}
	return []byte(result.Body), nil
}
