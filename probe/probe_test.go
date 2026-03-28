package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/platform/bilibili"
	"github.com/go-rod/rod/lib/proto"
)

// TestProbeAuthorPage opens a real Bilibili author's space page and dumps
// all network responses to understand what data is available and where.
// Run with: go test -v -run TestProbeAuthorPage -timeout 120s ./probe/
func TestProbeAuthorPage(t *testing.T) {
	// Load config for browser settings and cookie.
	if err := config.Load("../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()

	// Init debug log.
	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	// Create browser manager with concurrency=1.
	mgr, err := browser.New(browser.Config{
		Headless:    cfg.Browser.IsHeadless(),
		UserDataDir: cfg.Browser.UserDataDir,
		Concurrency: 1,
		BrowserBin:  cfg.Browser.Bin,
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer mgr.Close()

	// Inject cookie.
	page := mgr.GetPage()
	defer mgr.PutPage(page)

	if cfg.Cookie != "" {
		if err := browser.InjectCookie(page, ".bilibili.com", cfg.Cookie); err != nil {
			t.Logf("WARN: inject cookie: %v", err)
		} else {
			t.Log("Cookie injected successfully")
		}
	}

	// Enable network domain.
	_ = proto.NetworkEnable{}.Call(page)

	// Collect API responses.
	type apiResp struct {
		URL    string `json:"url"`
		Status int    `json:"status"`
		Size   int    `json:"body_size"`
	}
	var apiResponses []apiResp

	// Listen for network responses.
	go page.EachEvent(func(e *proto.NetworkResponseReceived) bool {
		url := e.Response.URL

		// Only care about API calls.
		isAPI := strings.Contains(url, "api.bilibili.com") ||
			strings.Contains(url, "/x/") ||
			strings.Contains(url, "space.bilibili.com/ajax")

		if isAPI {
			body, err := proto.NetworkGetResponseBody{RequestID: e.RequestID}.Call(page)
			bodySize := 0
			if err == nil && body != nil {
				bodySize = len(body.Body)
				// Log the first 500 chars of each API response.
				preview := body.Body
				if len(preview) > 500 {
					preview = preview[:500]
				}
				t.Logf("[API] %s\n  status=%d, size=%d\n  preview: %s", url, e.Response.Status, bodySize, preview)
			} else {
				t.Logf("[API] %s (body unavailable: %v)", url, err)
			}
			apiResponses = append(apiResponses, apiResp{URL: url, Status: e.Response.Status, Size: bodySize})
		}
		return false
	})()

	// Navigate to author's space page.
	mid := "314216"
	targetURL := fmt.Sprintf("https://space.bilibili.com/%s", mid)
	t.Logf("Navigating to %s", targetURL)

	if err := page.Navigate(targetURL); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Logf("WARN: wait load: %v", err)
	}

	t.Log("Page loaded, waiting 10 seconds for API calls...")
	time.Sleep(10 * time.Second)

	// Try to extract SSR data.
	t.Log("Checking SSR global variables...")
	ssrVars := []string{"__pinia", "__INITIAL_STATE__", "__NEXT_DATA__", "__NUXT__"}
	for _, v := range ssrVars {
		expr := fmt.Sprintf(`() => { try { let d = window.%s; if (!d) return ''; return JSON.stringify(d).substring(0, 1000); } catch(e) { return 'ERROR: ' + e.message; } }`, v)
		result, err := page.Eval(expr)
		if err != nil {
			t.Logf("SSR %s: eval error: %v", v, err)
			continue
		}
		str := result.Value.Str()
		if str == "" {
			t.Logf("SSR %s: not found (empty)", v)
		} else {
			t.Logf("SSR %s: %s", v, str)
		}
	}

	// Also try to extract __RENDER_DATA__ which some Bilibili pages use.
	renderExpr := `() => {
		try {
			let scripts = document.querySelectorAll('script');
			for (let s of scripts) {
				let text = s.textContent || '';
				if (text.includes('__INITIAL_STATE__') || text.includes('__pinia') || text.includes('__RENDER_DATA__')) {
					return text.substring(0, 2000);
				}
			}
			return 'no matching script found';
		} catch(e) { return 'ERROR: ' + e.message; }
	}`
	result, err := page.Eval(renderExpr)
	if err != nil {
		t.Logf("Script scan: error: %v", err)
	} else {
		t.Logf("Script scan: %s", result.Value.Str())
	}

	// Summary.
	t.Logf("\n=== SUMMARY: %d API responses captured ===", len(apiResponses))
	for i, r := range apiResponses {
		t.Logf("  [%d] %s (status=%d, size=%d)", i, r.URL, r.Status, r.Size)
	}

	// Save summary to file.
	summaryData, _ := json.MarshalIndent(apiResponses, "", "  ")
	os.MkdirAll("../data/probe", 0o755)
	os.WriteFile("../data/probe/api_responses.json", summaryData, 0o644)
	t.Logf("Summary saved to data/probe/api_responses.json")
}

// TestProbeStage1Quick does a quick Stage 1 test with 1 author.
// Run with: go test -v -run TestProbeStage1Quick -timeout 120s ./probe/
func TestProbeStage1Quick(t *testing.T) {
	// Load config.
	if err := config.Load("../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()

	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	mgr, err := browser.New(browser.Config{
		Headless:    cfg.Browser.IsHeadless(),
		UserDataDir: cfg.Browser.UserDataDir,
		Concurrency: 1,
		BrowserBin:  cfg.Browser.Bin,
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer mgr.Close()

	// Ensure login.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := browser.EnsureLogin(ctx, mgr, "https://www.bilibili.com", cfg.Cookie, bilibili.BilibiliLoginChecker); err != nil {
		t.Fatalf("ensure login: %v", err)
	}

	// Create author crawler.
	ac := bilibili.NewAuthorCrawler(mgr)

	// Test FetchAuthorInfo.
	mid := "314216"
	t.Logf("=== Testing FetchAuthorInfo for mid=%s ===", mid)
	info, err := ac.FetchAuthorInfo(ctx, mid)
	if err != nil {
		t.Fatalf("FetchAuthorInfo failed: %v", err)
	}
	t.Logf("Author info: name=%s, followers=%d", info.Name, info.Followers)

	// Test FetchAllAuthorVideos.
	t.Logf("=== Testing FetchAllAuthorVideos for mid=%s ===", mid)
	videos, pageInfo, err := ac.FetchAllAuthorVideos(ctx, mid, 100)
	if err != nil {
		t.Fatalf("FetchAllAuthorVideos failed: %v", err)
	}
	t.Logf("Videos: count=%d, totalPages=%d, totalCount=%d", len(videos), pageInfo.TotalPages, pageInfo.TotalCount)
	for i, v := range videos {
		if i >= 5 {
			t.Logf("  ... and %d more", len(videos)-5)
			break
		}
		t.Logf("  [%d] %s (bvid=%s, play=%d, comments=%d)", i, v.Title, v.BvID, v.PlayCount, v.CommentCount)
	}
}

// TestStage1EndToEnd tests the full Stage 1 flow with multiple authors.
// Run with: go test -v -run TestStage1EndToEnd -timeout 300s ./probe/
func TestStage1EndToEnd(t *testing.T) {
	// Load config.
	if err := config.Load("../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()

	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	mgr, err := browser.New(browser.Config{
		Headless:    cfg.Browser.IsHeadless(),
		UserDataDir: cfg.Browser.UserDataDir,
		Concurrency: 1,
		BrowserBin:  cfg.Browser.Bin,
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer mgr.Close()

	// Ensure login.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	if err := browser.EnsureLogin(ctx, mgr, "https://www.bilibili.com", cfg.Cookie, bilibili.BilibiliLoginChecker); err != nil {
		t.Fatalf("ensure login: %v", err)
	}

	ac := bilibili.NewAuthorCrawler(mgr)

	// Test with 3 authors from the intermediate data.
	mids := []struct {
		name string
		id   string
	}{
		{"随義のfreely", "314216"},
		{"老番茄", "546195"},
		{"影视飓风", "946974"},
	}

	for _, mid := range mids {
		t.Logf("\n=== Processing author: %s (mid=%s) ===", mid.name, mid.id)

		// FetchAuthorInfo
		info, err := ac.FetchAuthorInfo(ctx, mid.id)
		if err != nil {
			t.Errorf("FetchAuthorInfo failed for %s: %v", mid.name, err)
			continue
		}
		t.Logf("Author: name=%s, followers=%d", info.Name, info.Followers)

		// FetchAllAuthorVideos
		videos, pageInfo, err := ac.FetchAllAuthorVideos(ctx, mid.id, 100)
		if err != nil {
			t.Errorf("FetchAllAuthorVideos failed for %s: %v", mid.name, err)
			continue
		}
		t.Logf("Videos: count=%d, totalPages=%d, totalCount=%d", len(videos), pageInfo.TotalPages, pageInfo.TotalCount)

		if len(videos) > 0 {
			t.Logf("  First video: %s (bvid=%s, play=%d)", videos[0].Title, videos[0].BvID, videos[0].PlayCount)
		}
	}
}

// TestVerifyPagination verifies that FetchAllAuthorVideos returns multiple pages of data.
// Run with: go test -v -run TestVerifyPagination -timeout 120s ./probe/
func TestVerifyPagination(t *testing.T) {
	if err := config.Load("../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()

	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	mgr, err := browser.New(browser.Config{
		Headless:    cfg.Browser.IsHeadless(),
		UserDataDir: cfg.Browser.UserDataDir,
		Concurrency: 1,
		BrowserBin:  cfg.Browser.Bin,
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := browser.EnsureLogin(ctx, mgr, "https://www.bilibili.com", cfg.Cookie, bilibili.BilibiliLoginChecker); err != nil {
		t.Fatalf("ensure login: %v", err)
	}

	ac := bilibili.NewAuthorCrawler(mgr)
	mid := "314216" // 随義のfreely, 350 videos, 9 pages

	// Fetch all videos with a reasonable cap.
	videos, pageInfo, err := ac.FetchAllAuthorVideos(ctx, mid, 200)
	if err != nil {
		t.Fatalf("FetchAllAuthorVideos: %v", err)
	}
	t.Logf("Total videos fetched: %d, totalPages=%d, totalCount=%d", len(videos), pageInfo.TotalPages, pageInfo.TotalCount)

	// Verify we got more than one page worth of data.
	if len(videos) <= 30 {
		t.Errorf("Expected more than 30 videos (multiple pages), got %d", len(videos))
	}

	// Verify no duplicate BvIDs.
	seen := make(map[string]bool)
	for _, v := range videos {
		if seen[v.BvID] {
			t.Errorf("Duplicate BvID found: %s", v.BvID)
		}
		seen[v.BvID] = true
	}
	t.Logf("All %d videos have unique BvIDs ✓", len(videos))
}

// TestProbeSearchHashKeyword probes the difference between searching with "#keyword"
// vs "keyword" on Bilibili. The "#" character has special meaning in URLs (fragment identifier),
// and may also have special meaning on Bilibili (tag search vs keyword search).
//
// This probe tests 3 variants:
//  1. "宠物"       — plain keyword search
//  2. "#宠物"      — with literal "#" in URL (browser may treat as fragment)
//  3. "%23宠物"    — with URL-encoded "#" (%23)
//
// Run with: go test -v -run TestProbeSearchHashKeyword -timeout 120s ./probe/
func TestProbeSearchHashKeyword(t *testing.T) {
	// Load config for browser settings and cookie.
	if err := config.Load("../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()

	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	mgr, err := browser.New(browser.Config{
		Headless:    cfg.Browser.IsHeadless(),
		UserDataDir: cfg.Browser.UserDataDir,
		Concurrency: 1,
		BrowserBin:  cfg.Browser.Bin,
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer mgr.Close()

	page := mgr.GetPage()
	defer mgr.PutPage(page)

	if cfg.Cookie != "" {
		if err := browser.InjectCookie(page, ".bilibili.com", cfg.Cookie); err != nil {
			t.Logf("WARN: inject cookie: %v", err)
		} else {
			t.Log("Cookie injected successfully")
		}
	}

	// Define the 3 search URL variants to probe.
	variants := []struct {
		name string
		url  string
	}{
		{
			name: "plain_keyword",
			url:  "https://search.bilibili.com/video?keyword=宠物&page=1",
		},
		{
			name: "hash_literal",
			url:  "https://search.bilibili.com/video?keyword=#宠物&page=1",
		},
		{
			name: "hash_encoded",
			url:  "https://search.bilibili.com/video?keyword=%23宠物&page=1",
		},
	}

	// JS expression to extract Pinia search data + the actual URL the browser navigated to.
	probeJS := `() => {
		const result = {
			actualURL: window.location.href,
			actualSearch: window.location.search,
			actualHash: window.location.hash,
		};

		const pinia = window.__pinia;
		if (!pinia) {
			result.piniaExists = false;
			return JSON.stringify(result);
		}
		result.piniaExists = true;
		result.piniaKeys = Object.keys(pinia);

		const str = pinia.searchTypeResponse && pinia.searchTypeResponse.searchTypeResponse;
		if (str) {
			try {
				const data = JSON.parse(JSON.stringify(str));
				result.numPages = data.numPages || 0;
				result.numResults = data.numResults || 0;
				result.resultCount = (data.result && data.result.length) || 0;
				// Include first 3 result titles for comparison.
				if (data.result && data.result.length > 0) {
					result.sampleTitles = data.result.slice(0, 3).map(r => r.title);
				}
			} catch(e) {
				result.parseError = e.message;
			}
		} else {
			result.searchTypeResponse = null;
			// Dump all pinia keys for debugging.
			result.piniaDebug = {};
			for (const key of Object.keys(pinia)) {
				try {
					result.piniaDebug[key] = Object.keys(pinia[key]);
				} catch(e) {}
			}
		}

		return JSON.stringify(result);
	}`

	for _, v := range variants {
		t.Logf("\n========== Probing: %s ==========", v.name)
		t.Logf("URL: %s", v.url)

		_, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		if err := page.Navigate(v.url); err != nil {
			t.Errorf("[%s] navigate error: %v", v.name, err)
			cancel()
			continue
		}
		if err := page.WaitLoad(); err != nil {
			t.Logf("[%s] WARN: wait load: %v", v.name, err)
		}

		// Small delay to ensure Pinia state is populated.
		time.Sleep(2 * time.Second)

		result, err := page.Eval(probeJS)
		if err != nil {
			t.Errorf("[%s] eval error: %v", v.name, err)
			cancel()
			continue
		}

		raw := result.Value.Str()
		t.Logf("[%s] Raw result (%d bytes): %s", v.name, len(raw), raw)

		// Parse and pretty-print the result.
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			pretty, _ := json.MarshalIndent(parsed, "  ", "  ")
			t.Logf("[%s] Parsed result:\n  %s", v.name, string(pretty))
		}

		cancel()

		// Brief pause between requests to avoid rate limiting.
		time.Sleep(3 * time.Second)
	}

	t.Log("\n========== Probe Complete ==========")
	t.Log("Compare the results above to understand the difference between # and non-# keywords.")
}

// TestProbeSearchSpecialChars verifies that SearchPage correctly handles keywords
// containing URL-special characters (#, &, +, %, space, etc.) after the url.QueryEscape fix.
// This test uses the actual SearchPage method (not raw URL navigation) to confirm
// the fix works end-to-end.
//
// Run with: go test -v -run TestProbeSearchSpecialChars -timeout 180s ./probe/
func TestProbeSearchSpecialChars(t *testing.T) {
	if err := config.Load("../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()

	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	mgr, err := browser.New(browser.Config{
		Headless:    cfg.Browser.IsHeadless(),
		UserDataDir: cfg.Browser.UserDataDir,
		Concurrency: 1,
		BrowserBin:  cfg.Browser.Bin,
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer mgr.Close()

	// Ensure login for search.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := browser.EnsureLogin(ctx, mgr, "https://www.bilibili.com", cfg.Cookie, bilibili.BilibiliLoginChecker); err != nil {
		t.Fatalf("ensure login: %v", err)
	}

	sc := bilibili.NewSearchCrawler(mgr)

	// Test keywords with various special characters.
	// Each entry: keyword, description, whether we expect results (true = should have results).
	testCases := []struct {
		keyword     string
		description string
		expectData  bool
	}{
		{"#宠物", "hash prefix (tag-style keyword)", true},
		{"猫&狗", "ampersand in keyword", true},
		{"C++编程", "plus signs in keyword", true},
		{"100%好评", "percent sign in keyword", true},
		{"宠物 猫", "space in keyword", true},
		{"什么?", "question mark in keyword", true},
	}

	for _, tc := range testCases {
		t.Logf("\n========== Testing keyword: %q (%s) ==========", tc.keyword, tc.description)

		videos, pageInfo, err := sc.SearchPage(ctx, tc.keyword, 1)
		if err != nil {
			if tc.expectData {
				t.Errorf("[%s] keyword=%q: unexpected error: %v", tc.description, tc.keyword, err)
			} else {
				t.Logf("[%s] keyword=%q: expected error: %v", tc.description, tc.keyword, err)
			}
			// Pause between requests to avoid rate limiting.
			time.Sleep(3 * time.Second)
			continue
		}

		t.Logf("[%s] keyword=%q: videos=%d, totalPages=%d, totalCount=%d",
			tc.description, tc.keyword, len(videos), pageInfo.TotalPages, pageInfo.TotalCount)

		if tc.expectData && len(videos) == 0 {
			t.Errorf("[%s] keyword=%q: expected results but got 0 videos", tc.description, tc.keyword)
		}

		if len(videos) > 0 {
			// Show first 2 titles for verification.
			for i, v := range videos {
				if i >= 2 {
					break
				}
				t.Logf("  [%d] %s (author=%s, play=%d)", i, v.Title, v.Author, v.PlayCount)
			}
		}

		// Pause between requests to avoid rate limiting.
		time.Sleep(3 * time.Second)
	}

	t.Log("\n========== Special Character Probe Complete ==========")
}
