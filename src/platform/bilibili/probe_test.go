package bilibili

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
	"github.com/go-rod/rod/lib/proto"
)

// TestProbeAuthorPage opens a real Bilibili author's space page and dumps
// all network responses to understand what data is available and where.
// Run with: go test -v -run TestProbeAuthorPage -timeout 120s ./src/platform/bilibili/
func TestProbeAuthorPage(t *testing.T) {
	// Load config for browser settings and cookie.
	if err := config.Load("../../../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()

	// Init debug log.
	browser.InitDebugLog("../../../log")
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
	os.MkdirAll("../../../data/probe", 0o755)
	os.WriteFile("../../../data/probe/api_responses.json", summaryData, 0o644)
	t.Logf("Summary saved to data/probe/api_responses.json")
}

// TestProbeStage1Quick does a quick Stage 1 test with 1 author.
// Run with: go test -v -run TestProbeStage1Quick -timeout 120s ./src/platform/bilibili/
func TestProbeStage1Quick(t *testing.T) {
	// Load config.
	if err := config.Load("../../../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()

	browser.InitDebugLog("../../../log")
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

	if err := browser.EnsureLogin(ctx, mgr, "https://www.bilibili.com", cfg.Cookie, BilibiliLoginChecker); err != nil {
		t.Fatalf("ensure login: %v", err)
	}

	// Create author crawler.
	ac := NewAuthorCrawler(mgr)

	// Test FetchAuthorInfo.
	mid := "314216"
	t.Logf("=== Testing FetchAuthorInfo for mid=%s ===", mid)
	info, err := ac.FetchAuthorInfo(ctx, mid)
	if err != nil {
		t.Fatalf("FetchAuthorInfo failed: %v", err)
	}
	t.Logf("Author info: name=%s, followers=%d, region=%s", info.Name, info.Followers, info.Region)

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
// Run with: go test -v -run TestStage1EndToEnd -timeout 300s ./src/platform/bilibili/
func TestStage1EndToEnd(t *testing.T) {
	// Load config.
	if err := config.Load("../../../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()

	browser.InitDebugLog("../../../log")
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

	if err := browser.EnsureLogin(ctx, mgr, "https://www.bilibili.com", cfg.Cookie, BilibiliLoginChecker); err != nil {
		t.Fatalf("ensure login: %v", err)
	}

	ac := NewAuthorCrawler(mgr)

	// Test with 3 authors from the intermediate data.
	mids := []struct {
		name string
		id   string
	}{
		{"随义のfreely", "314216"},
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
// Run with: go test -v -run TestVerifyPagination -timeout 120s ./src/platform/bilibili/
func TestVerifyPagination(t *testing.T) {
	if err := config.Load("../../../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()

	browser.InitDebugLog("../../../log")
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

	if err := browser.EnsureLogin(ctx, mgr, "https://www.bilibili.com", cfg.Cookie, BilibiliLoginChecker); err != nil {
		t.Fatalf("ensure login: %v", err)
	}

	ac := NewAuthorCrawler(mgr)
	mid := "314216" // 随义のfreely, 350 videos, 9 pages

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
