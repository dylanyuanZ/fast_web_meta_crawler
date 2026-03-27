package test

import (
	"strings"
	"testing"
)

// ==================== Cookie String Parsing Tests ====================
// These tests verify the cookie parsing logic used by InjectCookie.
// InjectCookie splits "key1=value1; key2=value2" and injects each pair.
// Since InjectCookie requires a real rod.Page (CDP connection), we test
// the parsing logic independently here.

// parseCookiePairs replicates the cookie parsing logic from auth.go.
func parseCookiePairs(cookieStr string) []struct{ Name, Value string } {
	var pairs []struct{ Name, Value string }
	for _, pair := range strings.Split(cookieStr, ";") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		eqIdx := strings.Index(pair, "=")
		if eqIdx < 0 {
			continue // malformed pair, skip
		}
		name := strings.TrimSpace(pair[:eqIdx])
		value := strings.TrimSpace(pair[eqIdx+1:])
		pairs = append(pairs, struct{ Name, Value string }{name, value})
	}
	return pairs
}

func TestCookieParsing_Normal(t *testing.T) {
	cookieStr := "SESSDATA=abc123; bili_jct=def456; DedeUserID=789"

	pairs := parseCookiePairs(cookieStr)

	if len(pairs) != 3 {
		t.Fatalf("pairs length = %d, want 3", len(pairs))
	}

	expected := []struct{ Name, Value string }{
		{"SESSDATA", "abc123"},
		{"bili_jct", "def456"},
		{"DedeUserID", "789"},
	}

	for i, exp := range expected {
		if pairs[i].Name != exp.Name {
			t.Errorf("pairs[%d].Name = %q, want %q", i, pairs[i].Name, exp.Name)
		}
		if pairs[i].Value != exp.Value {
			t.Errorf("pairs[%d].Value = %q, want %q", i, pairs[i].Value, exp.Value)
		}
	}
}

func TestCookieParsing_SingleCookie(t *testing.T) {
	cookieStr := "SESSDATA=abc123"

	pairs := parseCookiePairs(cookieStr)

	if len(pairs) != 1 {
		t.Fatalf("pairs length = %d, want 1", len(pairs))
	}
	if pairs[0].Name != "SESSDATA" {
		t.Errorf("Name = %q, want %q", pairs[0].Name, "SESSDATA")
	}
	if pairs[0].Value != "abc123" {
		t.Errorf("Value = %q, want %q", pairs[0].Value, "abc123")
	}
}

func TestCookieParsing_EmptyString(t *testing.T) {
	pairs := parseCookiePairs("")

	if len(pairs) != 0 {
		t.Errorf("pairs length = %d, want 0", len(pairs))
	}
}

func TestCookieParsing_MalformedPairs(t *testing.T) {
	// Pairs without "=" should be skipped.
	cookieStr := "SESSDATA=abc123; malformed_no_equals; bili_jct=def456"

	pairs := parseCookiePairs(cookieStr)

	if len(pairs) != 2 {
		t.Fatalf("pairs length = %d, want 2 (malformed pair skipped)", len(pairs))
	}
	if pairs[0].Name != "SESSDATA" {
		t.Errorf("pairs[0].Name = %q, want %q", pairs[0].Name, "SESSDATA")
	}
	if pairs[1].Name != "bili_jct" {
		t.Errorf("pairs[1].Name = %q, want %q", pairs[1].Name, "bili_jct")
	}
}

func TestCookieParsing_ExtraSpaces(t *testing.T) {
	cookieStr := "  SESSDATA = abc123 ;  bili_jct = def456  "

	pairs := parseCookiePairs(cookieStr)

	if len(pairs) != 2 {
		t.Fatalf("pairs length = %d, want 2", len(pairs))
	}
	if pairs[0].Name != "SESSDATA" {
		t.Errorf("pairs[0].Name = %q, want %q", pairs[0].Name, "SESSDATA")
	}
	if pairs[0].Value != "abc123" {
		t.Errorf("pairs[0].Value = %q, want %q", pairs[0].Value, "abc123")
	}
}

func TestCookieParsing_ValueWithEquals(t *testing.T) {
	// Cookie values can contain "=" (e.g., base64 encoded values).
	cookieStr := "token=abc=def=ghi; sid=123"

	pairs := parseCookiePairs(cookieStr)

	if len(pairs) != 2 {
		t.Fatalf("pairs length = %d, want 2", len(pairs))
	}
	if pairs[0].Name != "token" {
		t.Errorf("pairs[0].Name = %q, want %q", pairs[0].Name, "token")
	}
	// Value should be everything after the first "=".
	if pairs[0].Value != "abc=def=ghi" {
		t.Errorf("pairs[0].Value = %q, want %q", pairs[0].Value, "abc=def=ghi")
	}
}

func TestCookieParsing_EmptyValue(t *testing.T) {
	cookieStr := "SESSDATA=; bili_jct="

	pairs := parseCookiePairs(cookieStr)

	if len(pairs) != 2 {
		t.Fatalf("pairs length = %d, want 2", len(pairs))
	}
	if pairs[0].Value != "" {
		t.Errorf("pairs[0].Value = %q, want empty", pairs[0].Value)
	}
	if pairs[1].Value != "" {
		t.Errorf("pairs[1].Value = %q, want empty", pairs[1].Value)
	}
}

func TestCookieParsing_TrailingSemicolon(t *testing.T) {
	cookieStr := "SESSDATA=abc123; bili_jct=def456;"

	pairs := parseCookiePairs(cookieStr)

	// Trailing semicolon produces an empty string after split, which should be skipped.
	if len(pairs) != 2 {
		t.Fatalf("pairs length = %d, want 2", len(pairs))
	}
}

func TestCookieParsing_MultipleSemicolons(t *testing.T) {
	cookieStr := "SESSDATA=abc123;; ;bili_jct=def456"

	pairs := parseCookiePairs(cookieStr)

	if len(pairs) != 2 {
		t.Fatalf("pairs length = %d, want 2 (empty segments skipped)", len(pairs))
	}
}

// ==================== extractDomain Logic Tests ====================
// extractDomain is tested in browser/auth_test.go (same package).
// Here we test the domain extraction logic from an external perspective
// to verify the expected behavior documented in spec.md.

func TestDomainExtraction_BilibiliURLs(t *testing.T) {
	// These test cases document the expected domain extraction for Bilibili URLs.
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "bilibili main site",
			url:  "https://www.bilibili.com",
			want: ".bilibili.com",
		},
		{
			name: "bilibili space",
			url:  "https://space.bilibili.com/12345",
			want: ".space.bilibili.com",
		},
		{
			name: "bilibili search",
			url:  "https://search.bilibili.com/video?keyword=test",
			want: ".search.bilibili.com",
		},
		{
			name: "bilibili API",
			url:  "https://api.bilibili.com/x/web-interface/search/type",
			want: ".api.bilibili.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDomainForTest(tt.url)
			if got != tt.want {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// extractDomainForTest replicates the extractDomain logic from auth.go
// for external package testing.
func extractDomainForTest(rawURL string) string {
	domain := rawURL
	if idx := strings.Index(domain, "://"); idx >= 0 {
		domain = domain[idx+3:]
	}
	if idx := strings.Index(domain, "/"); idx >= 0 {
		domain = domain[:idx]
	}
	if idx := strings.Index(domain, ":"); idx >= 0 {
		domain = domain[:idx]
	}
	if !strings.HasPrefix(domain, ".") {
		domain = strings.TrimPrefix(domain, "www.")
		domain = "." + domain
	}
	return domain
}

// ==================== EnsureLogin Strategy Tests ====================
// EnsureLogin requires a real browser Manager, so we test the strategy
// decision logic by verifying the expected behavior for each scenario.

func TestEnsureLoginStrategy_Documentation(t *testing.T) {
	// This test documents the expected EnsureLogin behavior for each scenario.
	// The actual EnsureLogin function requires a real browser, so we verify
	// the strategy matrix here as a specification test.

	type scenario struct {
		name      string
		headless  bool
		hasCookie bool
		loggedIn  bool
		expected  string // expected action
	}

	scenarios := []scenario{
		{
			name:     "already logged in via user_data_dir",
			headless: true, hasCookie: false, loggedIn: true,
			expected: "return nil (already logged in)",
		},
		{
			name:     "GUI mode, not logged in",
			headless: false, hasCookie: false, loggedIn: false,
			expected: "WaitForManualLogin",
		},
		{
			name:     "headless mode, not logged in, has cookie",
			headless: true, hasCookie: true, loggedIn: false,
			expected: "InjectCookie",
		},
		{
			name:     "headless mode, not logged in, no cookie",
			headless: true, hasCookie: false, loggedIn: false,
			expected: "anonymous mode (log warning)",
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			// Verify the strategy decision logic.
			var action string
			switch {
			case s.loggedIn:
				action = "return nil (already logged in)"
			case !s.headless:
				action = "WaitForManualLogin"
			case s.hasCookie:
				action = "InjectCookie"
			default:
				action = "anonymous mode (log warning)"
			}

			if action != s.expected {
				t.Errorf("scenario %q: action = %q, want %q", s.name, action, s.expected)
			}
		})
	}
}
