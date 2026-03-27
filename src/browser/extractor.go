package browser

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-rod/rod"
)

// defaultExtractTimeout is the maximum time to wait for page navigation and data extraction.
const defaultExtractTimeout = 30 * time.Second

// NavigateAndExtract opens a URL in the given Page, waits for it to load,
// then executes a JS expression to extract data from the page (e.g. SSR __INITIAL_STATE__).
//
// This is designed for SSR (Server-Side Rendered) pages where the data is embedded
// in the HTML document via a global JS variable, rather than fetched via XHR/Fetch.
//
// Parameters:
//   - ctx: context for timeout/cancellation
//   - page: rod Page to navigate
//   - targetURL: the URL to navigate to
//   - jsExpr: JS expression that returns the desired data (e.g. "JSON.stringify(window.__INITIAL_STATE__)")
//
// Returns the JS evaluation result as a string (typically JSON).
func NavigateAndExtract(ctx context.Context, page *rod.Page, targetURL string, jsExpr string) (string, error) {
	// Apply default timeout if the caller's context has no deadline.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultExtractTimeout)
		defer cancel()
	}

	log.Printf("INFO: [browser] navigating to %s", targetURL)

	// Navigate to the target URL.
	if err := page.Navigate(targetURL); err != nil {
		return "", fmt.Errorf("navigate to %s: %w", targetURL, err)
	}

	log.Printf("INFO: [browser] navigate done, waiting for page load: %s", targetURL)

	// Wait for page to finish loading.
	if err := page.WaitLoad(); err != nil {
		return "", fmt.Errorf("wait for page load %s: %w", targetURL, err)
	}

	log.Printf("INFO: [browser] page loaded: %s", targetURL)

	// Execute JS to extract data from the page.
	result, err := page.Eval(jsExpr)
	if err != nil {
		return "", fmt.Errorf("eval JS on %s: %w", targetURL, err)
	}

	str := result.Value.Str()
	if str == "" {
		return "", fmt.Errorf("JS expression returned empty result on %s (expression: %s)", targetURL, jsExpr)
	}

	log.Printf("INFO: [browser] extracted %d bytes of data from %s", len(str), targetURL)
	return str, nil
}
