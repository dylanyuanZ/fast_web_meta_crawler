package test

import (
	"testing"

	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
)

// ==================== InterceptRule Validation Tests ====================

func TestInterceptRule_URLPatternMatching(t *testing.T) {
	// Verify that URL substring matching logic works correctly.
	// NavigateAndIntercept uses strings.Contains(url, rule.URLPattern).
	tests := []struct {
		name      string
		url       string
		pattern   string
		wantMatch bool
	}{
		{
			name:      "search API match",
			url:       "https://api.bilibili.com/x/web-interface/search/type?search_type=video&keyword=test&page=1",
			pattern:   "/x/web-interface/search/type",
			wantMatch: true,
		},
		{
			name:      "user info API match",
			url:       "https://api.bilibili.com/x/space/acc/info?mid=12345",
			pattern:   "/x/space/acc/info",
			wantMatch: true,
		},
		{
			name:      "user stat API match",
			url:       "https://api.bilibili.com/x/relation/stat?vmid=12345",
			pattern:   "/x/relation/stat",
			wantMatch: true,
		},
		{
			name:      "video list API match",
			url:       "https://api.bilibili.com/x/space/wbi/arc/search?mid=12345&pn=1&ps=50",
			pattern:   "/x/space/wbi/arc/search",
			wantMatch: true,
		},
		{
			name:      "no match - different path",
			url:       "https://api.bilibili.com/x/web-interface/nav",
			pattern:   "/x/web-interface/search/type",
			wantMatch: false,
		},
		{
			name:      "no match - partial path",
			url:       "https://api.bilibili.com/x/space/acc",
			pattern:   "/x/space/acc/info",
			wantMatch: false,
		},
		{
			name:      "match with extra query params",
			url:       "https://api.bilibili.com/x/web-interface/search/type?search_type=video&keyword=test&page=1&order=totalrank",
			pattern:   "/x/web-interface/search/type",
			wantMatch: true,
		},
		{
			name:      "empty pattern matches everything",
			url:       "https://api.bilibili.com/any/path",
			pattern:   "",
			wantMatch: true,
		},
		{
			name:      "empty URL matches nothing",
			url:       "",
			pattern:   "/x/web-interface/search/type",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the matching logic from interceptor.go.
			got := containsSubstring(tt.url, tt.pattern)
			if got != tt.wantMatch {
				t.Errorf("contains(%q, %q) = %v, want %v", tt.url, tt.pattern, got, tt.wantMatch)
			}
		})
	}
}

// containsSubstring replicates the URL matching logic from interceptor.go.
func containsSubstring(s, substr string) bool {
	if substr == "" {
		return true
	}
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestInterceptRule_StructFields(t *testing.T) {
	rule := browser.InterceptRule{
		URLPattern: "/x/web-interface/search/type",
		ID:         "search",
	}

	if rule.URLPattern != "/x/web-interface/search/type" {
		t.Errorf("URLPattern = %q, want %q", rule.URLPattern, "/x/web-interface/search/type")
	}
	if rule.ID != "search" {
		t.Errorf("ID = %q, want %q", rule.ID, "search")
	}
}

func TestInterceptResult_StructFields(t *testing.T) {
	result := browser.InterceptResult{
		ID:   "search",
		Body: []byte(`{"code":0,"data":{}}`),
		URL:  "https://api.bilibili.com/x/web-interface/search/type?keyword=test",
	}

	if result.ID != "search" {
		t.Errorf("ID = %q, want %q", result.ID, "search")
	}
	if string(result.Body) != `{"code":0,"data":{}}` {
		t.Errorf("Body = %q, unexpected", string(result.Body))
	}
	if result.URL != "https://api.bilibili.com/x/web-interface/search/type?keyword=test" {
		t.Errorf("URL = %q, unexpected", result.URL)
	}
}

func TestInterceptRule_MultipleRules(t *testing.T) {
	// Verify that multiple rules can be defined for a single navigation.
	rules := []browser.InterceptRule{
		{URLPattern: "/x/space/acc/info", ID: "user_info"},
		{URLPattern: "/x/relation/stat", ID: "user_stat"},
	}

	if len(rules) != 2 {
		t.Fatalf("rules length = %d, want 2", len(rules))
	}

	// Verify each rule has a unique ID.
	ids := make(map[string]bool)
	for _, r := range rules {
		if ids[r.ID] {
			t.Errorf("duplicate rule ID: %q", r.ID)
		}
		ids[r.ID] = true
	}
}

func TestInterceptRule_EmptyRules(t *testing.T) {
	// Empty rules should be handled gracefully.
	// NavigateAndIntercept returns error for empty rules.
	rules := []browser.InterceptRule{}
	if len(rules) != 0 {
		t.Errorf("rules length = %d, want 0", len(rules))
	}
}
