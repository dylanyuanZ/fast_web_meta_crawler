package bilibili

import "strings"

// IsRetryableError checks if the error is retryable for Bilibili platform.
// Retryable errors include:
//   - 412 risk control responses (anti-crawl mechanism)
//   - Intercept timeouts (often caused by rate limiting)
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "status=412") ||
		strings.Contains(errMsg, "intercept timeout")
}
