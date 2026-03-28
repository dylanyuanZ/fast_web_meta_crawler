package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/platform/bilibili"
)

// ============================================================================
// Field Availability Probe Tests
//
// Purpose: Verify that the CSV fields planned for Stage 1 (basic) and Stage 2
// (full) can actually be obtained from Bilibili APIs. This probe dumps raw JSON
// responses from the following APIs:
//
//   Stage 1 (basic author info):
//     1. /x/space/wbi/acc/info  — name, video_count(?)
//     2. /x/relation/stat       — follower count
//     3. /x/space/upstat        — total play count, total likes (🆕 to verify)
//
//   Stage 2 (video list fields):
//     4. /x/space/wbi/arc/search — share count, favorite count (🆕 to verify)
//
// Run: go test -v -run TestProbeFieldAvailability -timeout 120s ./probe/
// ============================================================================

// TestProbeFieldAvailability opens an author's space page, intercepts all 3 APIs
// for Stage 1, then navigates to the video tab to intercept the video list API.
// Dumps raw JSON for each API to verify field availability.
func TestProbeFieldAvailability(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := browser.EnsureLogin(ctx, mgr, "https://www.bilibili.com", cfg.Cookie, bilibili.BilibiliLoginChecker); err != nil {
		t.Fatalf("ensure login: %v", err)
	}

	// Use a well-known author for testing.
	mid := "314216" // 随義のfreely

	// ========== Part 1: Stage 1 APIs (author space page) ==========
	t.Log("╔══════════════════════════════════════════════════════════════╗")
	t.Log("║  Part 1: Stage 1 APIs — Author Space Page                  ║")
	t.Log("║  Intercepting: acc/info, relation/stat, upstat             ║")
	t.Log("╚══════════════════════════════════════════════════════════════╝")

	page := mgr.GetPage()

	targetURL := fmt.Sprintf("https://space.bilibili.com/%s", mid)

	rules := []browser.InterceptRule{
		{URLPattern: "/x/space/wbi/acc/info", ID: "acc_info"},
		{URLPattern: "/x/relation/stat", ID: "relation_stat"},
		{URLPattern: "/x/space/upstat", ID: "up_stat"},
	}

	results, err := browser.NavigateAndIntercept(ctx, page, targetURL, rules)
	if err != nil {
		mgr.PutPage(page)
		t.Fatalf("NavigateAndIntercept failed: %v", err)
	}

	// Dump each API response.
	for _, r := range results {
		t.Logf("\n=== API: %s ===", r.ID)
		if r.Body == nil {
			t.Logf("  ❌ NOT INTERCEPTED (body is nil)")
			continue
		}
		t.Logf("  ✅ Intercepted, body size: %d bytes", len(r.Body))

		// Pretty-print the JSON.
		var raw json.RawMessage
		if err := json.Unmarshal(r.Body, &raw); err != nil {
			t.Logf("  ⚠️ Not valid JSON: %v", err)
			t.Logf("  Raw (first 500 bytes): %s", truncate(string(r.Body), 500))
			continue
		}
		pretty, _ := json.MarshalIndent(raw, "  ", "  ")
		// Limit output to 3000 chars to avoid flooding.
		prettyStr := string(pretty)
		if len(prettyStr) > 3000 {
			prettyStr = prettyStr[:3000] + "\n  ... (truncated)"
		}
		t.Logf("  Response JSON:\n  %s", prettyStr)

		// For acc/info, specifically check for video count related fields.
		if r.ID == "acc_info" {
			t.Log("\n  --- Checking acc/info for video count fields ---")
			var accInfo map[string]interface{}
			json.Unmarshal(r.Body, &accInfo)
			if data, ok := accInfo["data"].(map[string]interface{}); ok {
				// Check known and potential video count fields.
				fieldsToCheck := []string{"name", "sign", "face", "level_info", "official", "vip", "archive_count", "article_count"}
				for _, f := range fieldsToCheck {
					if v, exists := data[f]; exists {
						t.Logf("    data.%s = %v", f, v)
					} else {
						t.Logf("    data.%s = (not present)", f)
					}
				}
			}
		}

		// For up_stat, specifically check for total play and likes fields.
		if r.ID == "up_stat" {
			t.Log("\n  --- Checking upstat for total play/likes fields ---")
			var upStat map[string]interface{}
			json.Unmarshal(r.Body, &upStat)
			if data, ok := upStat["data"].(map[string]interface{}); ok {
				t.Logf("    data keys: %v", mapKeys(data))
				if archive, ok := data["archive"].(map[string]interface{}); ok {
					t.Logf("    data.archive keys: %v", mapKeys(archive))
					t.Logf("    data.archive.view = %v (total play count)", archive["view"])
				}
				if likes, ok := data["likes"]; ok {
					t.Logf("    data.likes = %v (total likes)", likes)
				}
				if article, ok := data["article"].(map[string]interface{}); ok {
					t.Logf("    data.article keys: %v", mapKeys(article))
				}
			}
		}
	}

	mgr.PutPage(page)

	// Brief pause before Part 2.
	time.Sleep(2 * time.Second)

	// ========== Part 2: Stage 2 API (video list) ==========
	t.Log("\n╔══════════════════════════════════════════════════════════════╗")
	t.Log("║  Part 2: Stage 2 API — Video List                          ║")
	t.Log("║  Intercepting: arc/search (checking share/favorites)       ║")
	t.Log("╚══════════════════════════════════════════════════════════════╝")

	page2 := mgr.GetPage()

	videoURL := fmt.Sprintf("https://space.bilibili.com/%s/video", mid)

	videoRules := []browser.InterceptRule{
		{URLPattern: "/x/space/wbi/arc/search", ID: "video_list"},
	}

	videoResults, err := browser.NavigateAndIntercept(ctx, page2, videoURL, videoRules)
	if err != nil {
		mgr.PutPage(page2)
		t.Fatalf("NavigateAndIntercept (video) failed: %v", err)
	}

	for _, r := range videoResults {
		t.Logf("\n=== API: %s ===", r.ID)
		if r.Body == nil {
			t.Logf("  ❌ NOT INTERCEPTED")
			continue
		}
		t.Logf("  ✅ Intercepted, body size: %d bytes", len(r.Body))

		// Parse the video list response to inspect individual video fields.
		var videoResp map[string]interface{}
		if err := json.Unmarshal(r.Body, &videoResp); err != nil {
			t.Logf("  ⚠️ Not valid JSON: %v", err)
			continue
		}

		// Navigate to data.list.vlist and dump the first video's ALL fields.
		if data, ok := videoResp["data"].(map[string]interface{}); ok {
			// Check page info.
			if page, ok := data["page"].(map[string]interface{}); ok {
				t.Logf("  Page info: %v", page)
			}

			if list, ok := data["list"].(map[string]interface{}); ok {
				if vlist, ok := list["vlist"].([]interface{}); ok && len(vlist) > 0 {
					t.Logf("  Total videos in this page: %d", len(vlist))

					// Dump the FIRST video's complete field set.
					if firstVideo, ok := vlist[0].(map[string]interface{}); ok {
						t.Log("\n  --- First video: ALL fields ---")
						allFields := mapKeys(firstVideo)
						t.Logf("    Field names (%d): %v", len(allFields), allFields)

						// Pretty-print the first video.
						pretty, _ := json.MarshalIndent(firstVideo, "    ", "  ")
						t.Logf("    Full JSON:\n    %s", string(pretty))

						// Specifically check for share and favorites fields.
						t.Log("\n  --- Checking for share/favorites fields ---")
						shareFields := []string{"share", "share_count", "forward", "repost"}
						for _, f := range shareFields {
							if v, exists := firstVideo[f]; exists {
								t.Logf("    ✅ %s = %v", f, v)
							} else {
								t.Logf("    ❌ %s = (not present)", f)
							}
						}

						favFields := []string{"favorites", "favorite", "collect", "fav", "coin"}
						for _, f := range favFields {
							if v, exists := firstVideo[f]; exists {
								t.Logf("    ✅ %s = %v", f, v)
							} else {
								t.Logf("    ❌ %s = (not present)", f)
							}
						}

						// Also check like field.
						likeFields := []string{"like", "likes", "like_count"}
						for _, f := range likeFields {
							if v, exists := firstVideo[f]; exists {
								t.Logf("    ✅ %s = %v", f, v)
							} else {
								t.Logf("    ❌ %s = (not present)", f)
							}
						}
					}
				}
			}
		}
	}

	mgr.PutPage(page2)

	// ========== Summary ==========
	t.Log("\n╔══════════════════════════════════════════════════════════════╗")
	t.Log("║                    PROBE SUMMARY                           ║")
	t.Log("╠══════════════════════════════════════════════════════════════╣")
	t.Log("║  Check the output above to confirm:                        ║")
	t.Log("║  1. /x/space/upstat is intercepted? (total play + likes)   ║")
	t.Log("║  2. acc/info has archive_count? (video count)              ║")
	t.Log("║  3. arc/search vlist has share/favorites fields?           ║")
	t.Log("╚══════════════════════════════════════════════════════════════╝")
}

// mapKeys returns all keys of a map[string]interface{}.
func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// truncate returns the first n characters of s.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
