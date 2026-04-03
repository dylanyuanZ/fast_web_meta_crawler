package probe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
	"github.com/go-rod/rod/lib/proto"
)

// ============================================================
// YouTube Probe Tests
// Purpose: Verify YouTube page rendering mode (SSR vs CSR),
//          API request patterns, scroll-loading mechanism,
//          and available data fields.
// ============================================================

// resolveBrowserBin checks if the configured browser binary exists.
// If not, tries known fallback paths before returning empty string
// to let rod auto-download chromium.
func resolveBrowserBin(bin string) string {
	if bin != "" {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
		fmt.Printf("WARN: configured browser binary %q not found, trying fallbacks...\n", bin)
	}

	// Known fallback paths for chromium/chrome.
	// The wrapper script uses glibc 2.31 from Docker overlay to run chromium
	// on systems with older glibc (e.g., glibc 2.17 on tlinux 2.2).
	fallbacks := []string{
		"/data/workspace_vscode/fast_web_meta_crawler/scripts/chrome-wrapper.sh",
		"/data/workspace_vscode/chromium-latest-linux/1606873/chrome-linux/chrome",
		"/usr/bin/chromium-browser",
		"/usr/bin/chromium",
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
	}
	for _, fb := range fallbacks {
		if _, err := os.Stat(fb); err == nil {
			fmt.Printf("INFO: using fallback browser binary: %s\n", fb)
			return fb
		}
	}

	fmt.Println("WARN: no browser binary found, will let rod auto-download chromium")
	return ""
}

// TestProbeYouTubeSearchPage opens a YouTube search results page and:
// 1. Dumps all SSR global variables (ytInitialData, __INITIAL_DATA__, etc.)
// 2. Captures all network requests (especially youtubei/v1/search)
// 3. Scrolls down once to trigger lazy-loading and captures incremental API requests
// 4. Saves all captured data to data/probe/youtube_search/
//
// Run with: go test -v -run TestProbeYouTubeSearchPage -timeout 120s ./probe/
func TestProbeYouTubeSearchPage(t *testing.T) {
	// Load config for browser settings.
	if err := config.Load("../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()

	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	// Create browser manager with concurrency=1.
	mgr, err := browser.New(browser.Config{
		Headless:    cfg.Browser.IsHeadless(),
		UserDataDir: cfg.Browser.UserDataDir,
		Concurrency: 1,
		BrowserBin:  resolveBrowserBin(cfg.Browser.Bin),
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer mgr.Close()

	page := mgr.GetPage()
	defer mgr.PutPage(page)

	// Create output directory.
	dumpDir := filepath.Join("..", "data", "probe", "youtube_search")
	os.MkdirAll(dumpDir, 0o755)

	// Enable network domain to capture requests.
	_ = proto.NetworkEnable{}.Call(page)

	// Collect all API responses.
	type apiResp struct {
		URL    string `json:"url"`
		Status int    `json:"status"`
		Size   int    `json:"body_size"`
		Phase  string `json:"phase"` // "initial" or "after_scroll"
	}
	var allResponses []apiResp
	currentPhase := "initial"

	// Listen for network responses.
	go page.EachEvent(func(e *proto.NetworkResponseReceived) bool {
		url := e.Response.URL

		// Filter: only care about YouTube API calls and data requests.
		isAPI := strings.Contains(url, "youtubei/") ||
			strings.Contains(url, "/api/") ||
			strings.Contains(url, "youtube.com/results") ||
			strings.Contains(url, "youtube.com/youtubei") ||
			strings.Contains(url, "search?") ||
			strings.Contains(url, "/browse") ||
			strings.Contains(url, "/next")

		resp := apiResp{
			URL:    url,
			Status: e.Response.Status,
			Phase:  currentPhase,
		}

		if isAPI {
			// Try to get response body.
			body, err := proto.NetworkGetResponseBody{RequestID: e.RequestID}.Call(page)
			if err == nil && body != nil {
				resp.Size = len(body.Body)

				// Save response body to file.
				safeName := strings.ReplaceAll(url, "https://", "")
				safeName = strings.ReplaceAll(safeName, "http://", "")
				safeName = strings.ReplaceAll(safeName, "/", "_")
				safeName = strings.ReplaceAll(safeName, "?", "_Q_")
				safeName = strings.ReplaceAll(safeName, "&", "_A_")
				if len(safeName) > 150 {
					safeName = safeName[:150]
				}
				safeName = currentPhase + "_" + safeName + ".json"

				fpath := filepath.Join(dumpDir, safeName)
				os.WriteFile(fpath, []byte(body.Body), 0o644)
				t.Logf("[API][%s] %s → %d bytes → %s", currentPhase, url, len(body.Body), safeName)
			} else {
				t.Logf("[API][%s] %s → (body unavailable: %v)", currentPhase, url, err)
			}
		}

		allResponses = append(allResponses, resp)
		return false // keep listening
	})()

	// Navigate to YouTube search page.
	// Use "人福药业" as test keyword (moderate results, good for testing).
	searchURL := "https://www.youtube.com/results?search_query=%E4%BA%BA%E7%A6%8F%E8%8D%AF%E4%B8%9A"
	t.Logf("Navigating to %s", searchURL)

	if err := page.Navigate(searchURL); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Logf("WARN: wait load: %v", err)
	}

	t.Log("Page loaded, waiting 8 seconds for initial data...")
	time.Sleep(8 * time.Second)

	// ========== Check SSR Global Variables ==========
	t.Log("\n========== Checking SSR Global Variables ==========")

	ssrChecks := []struct {
		name string
		expr string
	}{
		// YouTube-specific SSR variables.
		{"ytInitialData", `() => { try { if (!window.ytInitialData) return ''; return JSON.stringify(window.ytInitialData).substring(0, 5000); } catch(e) { return 'ERROR: ' + e.message; } }`},
		{"ytInitialPlayerResponse", `() => { try { if (!window.ytInitialPlayerResponse) return ''; return JSON.stringify(window.ytInitialPlayerResponse).substring(0, 2000); } catch(e) { return 'ERROR: ' + e.message; } }`},
		{"ytcfg", `() => { try { if (!window.ytcfg) return ''; return JSON.stringify(window.ytcfg.data_).substring(0, 3000); } catch(e) { return 'ERROR: ' + e.message; } }`},
		// Generic SSR variables (check if YouTube uses any of these).
		{"__INITIAL_DATA__", `() => { try { if (!window.__INITIAL_DATA__) return ''; return JSON.stringify(window.__INITIAL_DATA__).substring(0, 2000); } catch(e) { return 'ERROR: ' + e.message; } }`},
		{"__NEXT_DATA__", `() => { try { if (!window.__NEXT_DATA__) return ''; return JSON.stringify(window.__NEXT_DATA__).substring(0, 2000); } catch(e) { return 'ERROR: ' + e.message; } }`},
		{"__pinia", `() => { try { if (!window.__pinia) return ''; return JSON.stringify(window.__pinia).substring(0, 2000); } catch(e) { return 'ERROR: ' + e.message; } }`},
	}

	for _, check := range ssrChecks {
		result, err := page.Eval(check.expr)
		if err != nil {
			t.Logf("SSR %s: eval error: %v", check.name, err)
			continue
		}
		str := result.Value.Str()
		if str == "" {
			t.Logf("SSR %s: NOT FOUND (empty)", check.name)
		} else if strings.HasPrefix(str, "ERROR:") {
			t.Logf("SSR %s: %s", check.name, str)
		} else {
			// Save full SSR data to file.
			fpath := filepath.Join(dumpDir, fmt.Sprintf("ssr_%s.json", check.name))
			os.WriteFile(fpath, []byte(str), 0o644)
			t.Logf("SSR %s: FOUND! %d chars → %s", check.name, len(str), fpath)
		}
	}

	// Also dump the FULL ytInitialData (not truncated) if it exists.
	fullYtInitialDataExpr := `() => {
		try {
			if (!window.ytInitialData) return '';
			return JSON.stringify(window.ytInitialData);
		} catch(e) { return 'ERROR: ' + e.message; }
	}`
	fullResult, err := page.Eval(fullYtInitialDataExpr)
	if err == nil {
		str := fullResult.Value.Str()
		if str != "" && !strings.HasPrefix(str, "ERROR:") {
			fpath := filepath.Join(dumpDir, "ssr_ytInitialData_FULL.json")
			os.WriteFile(fpath, []byte(str), 0o644)
			t.Logf("SSR ytInitialData (FULL): %d bytes → %s", len(str), fpath)
		}
	}

	// ========== Analyze ytInitialData structure ==========
	t.Log("\n========== Analyzing ytInitialData Structure ==========")

	analyzeExpr := `() => {
		try {
			if (!window.ytInitialData) return JSON.stringify({exists: false});
			const data = window.ytInitialData;
			const result = {
				exists: true,
				topLevelKeys: Object.keys(data),
			};

			// Check for search results in contents.
			if (data.contents) {
				result.contentsKeys = Object.keys(data.contents);
				const twoCol = data.contents.twoColumnSearchResultsRenderer;
				if (twoCol) {
					result.hasTwoColumnSearchResultsRenderer = true;
					if (twoCol.primaryContents) {
						const section = twoCol.primaryContents.sectionListRenderer;
						if (section && section.contents) {
							result.sectionCount = section.contents.length;
							// Analyze first section.
							const firstSection = section.contents[0];
							if (firstSection && firstSection.itemSectionRenderer) {
								const items = firstSection.itemSectionRenderer.contents;
								result.firstSectionItemCount = items ? items.length : 0;
								// Get types of first 5 items.
								if (items) {
									result.firstSectionItemTypes = items.slice(0, 10).map(item => Object.keys(item)[0]);
								}
								// Extract first video renderer details.
								for (const item of (items || [])) {
									if (item.videoRenderer) {
										const vr = item.videoRenderer;
										result.sampleVideoRenderer = {
											videoId: vr.videoId,
											title: vr.title && vr.title.runs ? vr.title.runs.map(r => r.text).join('') : '',
											viewCountText: vr.viewCountText ? (vr.viewCountText.simpleText || JSON.stringify(vr.viewCountText)) : '',
											publishedTimeText: vr.publishedTimeText ? vr.publishedTimeText.simpleText : '',
											lengthText: vr.lengthText ? vr.lengthText.simpleText : '',
											ownerText: vr.ownerText && vr.ownerText.runs ? vr.ownerText.runs.map(r => r.text).join('') : '',
											channelId: vr.ownerText && vr.ownerText.runs && vr.ownerText.runs[0] && vr.ownerText.runs[0].navigationEndpoint ? 
												vr.ownerText.runs[0].navigationEndpoint.browseEndpoint.browseId : '',
											descriptionSnippet: vr.detailedMetadataSnippets ? JSON.stringify(vr.detailedMetadataSnippets[0]).substring(0, 200) : '',
											thumbnailUrl: vr.thumbnail && vr.thumbnail.thumbnails ? vr.thumbnail.thumbnails[0].url : '',
											allKeys: Object.keys(vr),
										};
										break;
									}
								}
							}
						}
					}
				}
			}

			// Check for continuation token (for scroll loading).
			if (data.contents) {
				const twoCol = data.contents.twoColumnSearchResultsRenderer;
				if (twoCol && twoCol.primaryContents) {
					const section = twoCol.primaryContents.sectionListRenderer;
					if (section && section.contents) {
						for (const content of section.contents) {
							if (content.continuationItemRenderer) {
								result.hasContinuationToken = true;
								result.continuationEndpoint = content.continuationItemRenderer.continuationEndpoint ? 
									Object.keys(content.continuationItemRenderer.continuationEndpoint) : [];
								break;
							}
						}
					}
				}
			}

			return JSON.stringify(result);
		} catch(e) { return JSON.stringify({error: e.message, stack: e.stack}); }
	}`

	analyzeResult, err := page.Eval(analyzeExpr)
	if err != nil {
		t.Logf("Analyze ytInitialData: eval error: %v", err)
	} else {
		raw := analyzeResult.Value.Str()
		// Pretty print.
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			pretty, _ := json.MarshalIndent(parsed, "", "  ")
			t.Logf("ytInitialData analysis:\n%s", string(pretty))
			fpath := filepath.Join(dumpDir, "analysis_ytInitialData.json")
			os.WriteFile(fpath, pretty, 0o644)
		} else {
			t.Logf("ytInitialData analysis (raw): %s", raw)
		}
	}

	// ========== Scroll and Capture Incremental Requests ==========
	t.Log("\n========== Scrolling to trigger lazy-loading ==========")
	currentPhase = "after_scroll"

	// Scroll down 3 times to trigger loading.
	for i := 0; i < 3; i++ {
		_, err := page.Eval(`() => { window.scrollBy(0, 3000); }`)
		if err != nil {
			t.Logf("Scroll %d: error: %v", i+1, err)
		} else {
			t.Logf("Scroll %d: done", i+1)
		}
		time.Sleep(3 * time.Second)
	}

	t.Log("Waiting 5 seconds for scroll-triggered API calls...")
	time.Sleep(5 * time.Second)

	// ========== Check for "no more results" indicator ==========
	t.Log("\n========== Checking end-of-results indicator ==========")
	endCheckExpr := `() => {
		// Check for "No more results" message.
		const messages = document.querySelectorAll('yt-formatted-string, #message');
		const results = [];
		for (const el of messages) {
			const text = el.textContent.trim();
			if (text && (text.includes('更多') || text.includes('No more') || text.includes('no more') || text.includes('结果'))) {
				results.push({tag: el.tagName, id: el.id, text: text});
			}
		}
		return JSON.stringify({
			messageElements: results,
			totalVideoRenderers: document.querySelectorAll('ytd-video-renderer').length,
			totalShelfRenderers: document.querySelectorAll('ytd-shelf-renderer').length,
		});
	}`
	endResult, err := page.Eval(endCheckExpr)
	if err != nil {
		t.Logf("End check: error: %v", err)
	} else {
		t.Logf("End check: %s", endResult.Value.Str())
	}

	// ========== Summary ==========
	t.Logf("\n========== SUMMARY: %d total responses captured ==========", len(allResponses))
	apiCount := 0
	for _, r := range allResponses {
		if r.Size > 0 {
			apiCount++
			t.Logf("  [%s] %s (status=%d, size=%d)", r.Phase, r.URL, r.Status, r.Size)
		}
	}
	t.Logf("API responses with body: %d", apiCount)

	// Save summary.
	summaryData, _ := json.MarshalIndent(allResponses, "", "  ")
	fpath := filepath.Join(dumpDir, "_summary.json")
	os.WriteFile(fpath, summaryData, 0o644)
	t.Logf("Summary saved to %s", fpath)
}

// TestProbeYouTubeAuthorPage opens a YouTube channel page and:
// 1. Dumps SSR global variables (ytInitialData, etc.)
// 2. Captures network requests on the channel home page
// 3. Clicks "About" / "More" to see author details
// 4. Navigates to the Videos tab and captures video list data
// 5. Scrolls once on the Videos tab to check lazy-loading
//
// Run with: go test -v -run TestProbeYouTubeAuthorPage -timeout 120s ./probe/
func TestProbeYouTubeAuthorPage(t *testing.T) {
	// Load config for browser settings.
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
		BrowserBin:  resolveBrowserBin(cfg.Browser.Bin),
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer mgr.Close()

	page := mgr.GetPage()
	defer mgr.PutPage(page)

	// Create output directory.
	dumpDir := filepath.Join("..", "data", "probe", "youtube_author")
	os.MkdirAll(dumpDir, 0o755)

	// Enable network domain.
	_ = proto.NetworkEnable{}.Call(page)

	// Collect API responses.
	type apiResp struct {
		URL    string `json:"url"`
		Status int    `json:"status"`
		Size   int    `json:"body_size"`
		Phase  string `json:"phase"`
	}
	var allResponses []apiResp
	currentPhase := "channel_home"

	go page.EachEvent(func(e *proto.NetworkResponseReceived) bool {
		url := e.Response.URL

		isAPI := strings.Contains(url, "youtubei/") ||
			strings.Contains(url, "/api/") ||
			strings.Contains(url, "/browse") ||
			strings.Contains(url, "/next") ||
			strings.Contains(url, "youtube.com/@")

		resp := apiResp{
			URL:    url,
			Status: e.Response.Status,
			Phase:  currentPhase,
		}

		if isAPI {
			body, err := proto.NetworkGetResponseBody{RequestID: e.RequestID}.Call(page)
			if err == nil && body != nil {
				resp.Size = len(body.Body)

				safeName := strings.ReplaceAll(url, "https://", "")
				safeName = strings.ReplaceAll(safeName, "http://", "")
				safeName = strings.ReplaceAll(safeName, "/", "_")
				safeName = strings.ReplaceAll(safeName, "?", "_Q_")
				safeName = strings.ReplaceAll(safeName, "&", "_A_")
				if len(safeName) > 150 {
					safeName = safeName[:150]
				}
				safeName = currentPhase + "_" + safeName + ".json"

				fpath := filepath.Join(dumpDir, safeName)
				os.WriteFile(fpath, []byte(body.Body), 0o644)
				t.Logf("[API][%s] %s → %d bytes", currentPhase, url, len(body.Body))
			}
		}

		allResponses = append(allResponses, resp)
		return false
	})()

	// ========== Phase 1: Channel Home Page ==========
	// Use @BrunoMars as a well-known channel for testing.
	channelURL := "https://www.youtube.com/@BrunoMars"
	t.Logf("\n========== Phase 1: Channel Home Page ==========")
	t.Logf("Navigating to %s", channelURL)

	if err := page.Navigate(channelURL); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Logf("WARN: wait load: %v", err)
	}

	t.Log("Page loaded, waiting 8 seconds...")
	time.Sleep(8 * time.Second)

	// Check SSR variables.
	t.Log("Checking SSR global variables on channel page...")
	ssrChecks := []struct {
		name string
		expr string
	}{
		{"ytInitialData", `() => { try { if (!window.ytInitialData) return ''; return JSON.stringify(window.ytInitialData).substring(0, 5000); } catch(e) { return 'ERROR: ' + e.message; } }`},
		{"ytcfg", `() => { try { if (!window.ytcfg) return ''; let d = window.ytcfg.data_; return d ? JSON.stringify(d).substring(0, 3000) : ''; } catch(e) { return 'ERROR: ' + e.message; } }`},
	}

	for _, check := range ssrChecks {
		result, err := page.Eval(check.expr)
		if err != nil {
			t.Logf("SSR %s: eval error: %v", check.name, err)
			continue
		}
		str := result.Value.Str()
		if str == "" {
			t.Logf("SSR %s: NOT FOUND", check.name)
		} else if strings.HasPrefix(str, "ERROR:") {
			t.Logf("SSR %s: %s", check.name, str)
		} else {
			fpath := filepath.Join(dumpDir, fmt.Sprintf("ssr_%s_channel_home.json", check.name))
			os.WriteFile(fpath, []byte(str), 0o644)
			t.Logf("SSR %s: FOUND! %d chars", check.name, len(str))
		}
	}

	// Dump full ytInitialData for channel page.
	fullExpr := `() => { try { if (!window.ytInitialData) return ''; return JSON.stringify(window.ytInitialData); } catch(e) { return 'ERROR: ' + e.message; } }`
	fullResult, err := page.Eval(fullExpr)
	if err == nil {
		str := fullResult.Value.Str()
		if str != "" && !strings.HasPrefix(str, "ERROR:") {
			fpath := filepath.Join(dumpDir, "ssr_ytInitialData_channel_home_FULL.json")
			os.WriteFile(fpath, []byte(str), 0o644)
			t.Logf("SSR ytInitialData (FULL): %d bytes", len(str))
		}
	}

	// Analyze channel page ytInitialData structure.
	channelAnalyzeExpr := `() => {
		try {
			if (!window.ytInitialData) return JSON.stringify({exists: false});
			const data = window.ytInitialData;
			const result = {
				exists: true,
				topLevelKeys: Object.keys(data),
			};

			// Check metadata (channel info).
			if (data.metadata) {
				result.metadataKeys = Object.keys(data.metadata);
				const cr = data.metadata.channelMetadataRenderer;
				if (cr) {
					result.channelMetadata = {
						title: cr.title,
						description: cr.description ? cr.description.substring(0, 200) : '',
						externalId: cr.externalId,
						channelUrl: cr.channelUrl,
						vanityChannelUrl: cr.vanityChannelUrl,
						keywords: cr.keywords,
						allKeys: Object.keys(cr),
					};
				}
			}

			// Check header (subscriber count, etc.).
			if (data.header) {
				result.headerKeys = Object.keys(data.header);
				const ch = data.header.c4TabbedHeaderRenderer || data.header.pageHeaderRenderer;
				if (ch) {
					result.headerRendererType = data.header.c4TabbedHeaderRenderer ? 'c4TabbedHeaderRenderer' : 'pageHeaderRenderer';
					result.headerRendererKeys = Object.keys(ch);
					if (ch.subscriberCountText) {
						result.subscriberCountText = ch.subscriberCountText.simpleText || JSON.stringify(ch.subscriberCountText);
					}
					if (ch.videosCountText) {
						result.videosCountText = ch.videosCountText.simpleText || JSON.stringify(ch.videosCountText);
					}
				}
			}

			// Check tabs (Videos, Shorts, etc.).
			if (data.contents && data.contents.twoColumnBrowseResultsRenderer) {
				const tabs = data.contents.twoColumnBrowseResultsRenderer.tabs;
				if (tabs) {
					result.tabCount = tabs.length;
					result.tabTitles = tabs.map(tab => {
						if (tab.tabRenderer) return tab.tabRenderer.title;
						if (tab.expandableTabRenderer) return tab.expandableTabRenderer.title;
						return Object.keys(tab)[0];
					});
				}
			}

			// Check microformat (additional channel info).
			if (data.microformat) {
				result.microformatKeys = Object.keys(data.microformat);
				const mf = data.microformat.microformatDataRenderer;
				if (mf) {
					result.microformat = {
						allKeys: Object.keys(mf),
					};
				}
			}

			return JSON.stringify(result);
		} catch(e) { return JSON.stringify({error: e.message}); }
	}`

	channelAnalyzeResult, err := page.Eval(channelAnalyzeExpr)
	if err != nil {
		t.Logf("Channel analysis: eval error: %v", err)
	} else {
		raw := channelAnalyzeResult.Value.Str()
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			pretty, _ := json.MarshalIndent(parsed, "", "  ")
			t.Logf("Channel ytInitialData analysis:\n%s", string(pretty))
			fpath := filepath.Join(dumpDir, "analysis_channel_home.json")
			os.WriteFile(fpath, pretty, 0o644)
		} else {
			t.Logf("Channel analysis (raw): %s", raw)
		}
	}

	// ========== Phase 2: Videos Tab ==========
	t.Logf("\n========== Phase 2: Videos Tab ==========")
	currentPhase = "videos_tab"

	videosURL := "https://www.youtube.com/@BrunoMars/videos"
	t.Logf("Navigating to %s", videosURL)

	if err := page.Navigate(videosURL); err != nil {
		t.Logf("WARN: navigate to videos tab: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Logf("WARN: wait load: %v", err)
	}

	t.Log("Videos tab loaded, waiting 8 seconds...")
	time.Sleep(8 * time.Second)

	// Dump ytInitialData for videos tab.
	fullResult2, err := page.Eval(fullExpr)
	if err == nil {
		str := fullResult2.Value.Str()
		if str != "" && !strings.HasPrefix(str, "ERROR:") {
			fpath := filepath.Join(dumpDir, "ssr_ytInitialData_videos_tab_FULL.json")
			os.WriteFile(fpath, []byte(str), 0o644)
			t.Logf("SSR ytInitialData videos tab (FULL): %d bytes", len(str))
		}
	}

	// Analyze videos tab data.
	videosAnalyzeExpr := `() => {
		try {
			if (!window.ytInitialData) return JSON.stringify({exists: false});
			const data = window.ytInitialData;
			const result = {exists: true};

			// Find the Videos tab content.
			if (data.contents && data.contents.twoColumnBrowseResultsRenderer) {
				const tabs = data.contents.twoColumnBrowseResultsRenderer.tabs;
				for (const tab of (tabs || [])) {
					if (tab.tabRenderer && tab.tabRenderer.selected) {
						result.selectedTab = tab.tabRenderer.title;
						const content = tab.tabRenderer.content;
						if (content) {
							result.contentKeys = Object.keys(content);
							// Look for richGridRenderer (video grid).
							const grid = content.richGridRenderer;
							if (grid) {
								result.hasRichGridRenderer = true;
								result.gridContentCount = grid.contents ? grid.contents.length : 0;
								// Analyze first few items.
								if (grid.contents && grid.contents.length > 0) {
									result.gridItemTypes = grid.contents.slice(0, 5).map(item => Object.keys(item)[0]);
									// Extract first video details.
									for (const item of grid.contents) {
										const ri = item.richItemRenderer;
										if (ri && ri.content && ri.content.videoRenderer) {
											const vr = ri.content.videoRenderer;
											result.sampleVideo = {
												videoId: vr.videoId,
												title: vr.title && vr.title.runs ? vr.title.runs.map(r => r.text).join('') : '',
												viewCountText: vr.viewCountText ? (vr.viewCountText.simpleText || JSON.stringify(vr.viewCountText)) : '',
												publishedTimeText: vr.publishedTimeText ? vr.publishedTimeText.simpleText : '',
												lengthText: vr.lengthText ? vr.lengthText.simpleText : '',
												allKeys: Object.keys(vr),
											};
											break;
										}
									}
								}
								// Check for continuation (scroll loading).
								if (grid.contents) {
									const lastItem = grid.contents[grid.contents.length - 1];
									if (lastItem && lastItem.continuationItemRenderer) {
										result.hasContinuation = true;
									}
								}
							}
						}
						break;
					}
				}
			}

			return JSON.stringify(result);
		} catch(e) { return JSON.stringify({error: e.message}); }
	}`

	videosAnalyzeResult, err := page.Eval(videosAnalyzeExpr)
	if err != nil {
		t.Logf("Videos tab analysis: eval error: %v", err)
	} else {
		raw := videosAnalyzeResult.Value.Str()
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			pretty, _ := json.MarshalIndent(parsed, "", "  ")
			t.Logf("Videos tab analysis:\n%s", string(pretty))
			fpath := filepath.Join(dumpDir, "analysis_videos_tab.json")
			os.WriteFile(fpath, pretty, 0o644)
		} else {
			t.Logf("Videos tab analysis (raw): %s", raw)
		}
	}

	// ========== Phase 3: Scroll on Videos Tab ==========
	t.Logf("\n========== Phase 3: Scroll on Videos Tab ==========")
	currentPhase = "videos_scroll"

	for i := 0; i < 2; i++ {
		_, err := page.Eval(`() => { window.scrollBy(0, 3000); }`)
		if err != nil {
			t.Logf("Scroll %d: error: %v", i+1, err)
		} else {
			t.Logf("Scroll %d: done", i+1)
		}
		time.Sleep(3 * time.Second)
	}

	t.Log("Waiting 5 seconds for scroll-triggered requests...")
	time.Sleep(5 * time.Second)

	// ========== Summary ==========
	t.Logf("\n========== SUMMARY: %d total responses captured ==========", len(allResponses))
	apiCount := 0
	for _, r := range allResponses {
		if r.Size > 0 {
			apiCount++
			t.Logf("  [%s] %s (status=%d, size=%d)", r.Phase, r.URL, r.Status, r.Size)
		}
	}
	t.Logf("API responses with body: %d", apiCount)

	// Save summary.
	summaryData, _ := json.MarshalIndent(allResponses, "", "  ")
	fpath := filepath.Join(dumpDir, "_summary.json")
	os.WriteFile(fpath, summaryData, 0o644)
	t.Logf("Summary saved to %s", fpath)
}

// ============================================================
// Probe V2: Deep verification tests
// ============================================================

// TestProbeYouTubeScrollToBottom verifies how to detect "scrolled to bottom"
// on a YouTube search page with very few results.
// Uses "天齐锂业" + filter: Videos + This week → very few results.
//
// Run with: go test -v -run TestProbeYouTubeScrollToBottom -timeout 180s ./probe/
func TestProbeYouTubeScrollToBottom(t *testing.T) {
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
		BrowserBin:  resolveBrowserBin(cfg.Browser.Bin),
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer mgr.Close()

	page := mgr.GetPage()
	defer mgr.PutPage(page)

	dumpDir := filepath.Join("..", "data", "probe", "youtube_scroll_bottom")
	os.MkdirAll(dumpDir, 0o755)

	_ = proto.NetworkEnable{}.Call(page)

	type apiCapture struct {
		URL   string `json:"url"`
		Size  int    `json:"body_size"`
		Phase string `json:"phase"`
	}
	var apiCaptures []apiCapture
	scrollPhase := "initial"

	go page.EachEvent(func(e *proto.NetworkResponseReceived) bool {
		url := e.Response.URL
		if strings.Contains(url, "youtubei/v1/search") || strings.Contains(url, "youtubei/v1/browse") {
			body, err := proto.NetworkGetResponseBody{RequestID: e.RequestID}.Call(page)
			if err == nil && body != nil {
				apiCaptures = append(apiCaptures, apiCapture{URL: url, Size: len(body.Body), Phase: scrollPhase})
				safeName := fmt.Sprintf("%s_search_api_%d.json", scrollPhase, len(apiCaptures))
				fpath := filepath.Join(dumpDir, safeName)
				os.WriteFile(fpath, []byte(body.Body), 0o644)
				t.Logf("[API][%s] %s → %d bytes → %s", scrollPhase, url, len(body.Body), safeName)
			}
		}
		return false
	})()

	// Step 1: Navigate with search query.
	searchURL := "https://www.youtube.com/results?search_query=%E5%A4%A9%E9%BD%90%E9%94%82%E4%B8%9A"
	t.Logf("Step 1: Navigating to %s", searchURL)

	if err := page.Navigate(searchURL); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Logf("WARN: wait load: %v", err)
	}
	time.Sleep(8 * time.Second)

	// Step 2: Apply Videos filter, then extract combined filter values.
	filteredURL := "https://www.youtube.com/results?search_query=%E5%A4%A9%E9%BD%90%E9%94%82%E4%B8%9A&sp=EgIQAQ%253D%253D"
	t.Logf("Step 2: Navigating with Videos filter: %s", filteredURL)

	if err := page.Navigate(filteredURL); err != nil {
		t.Logf("WARN: navigate filtered: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Logf("WARN: wait load: %v", err)
	}
	time.Sleep(8 * time.Second)

	// Extract combined filter sp values from the filter dialog.
	combinedFilterExpr := `() => {
		try {
			const data = window.ytInitialData;
			if (!data || !data.header) return JSON.stringify({error: "no ytInitialData"});
			const dialog = data.header.searchHeaderRenderer.searchFilterButton.buttonRenderer.command.openPopupAction.popup.searchFilterOptionsDialogRenderer;
			const result = {};
			for (const group of dialog.groups) {
				const g = group.searchFilterGroupRenderer;
				const title = g.title.simpleText || '';
				for (const f of g.filters) {
					const fr = f.searchFilterRenderer;
					const label = fr.label ? fr.label.simpleText : '';
					const nav = fr.navigationEndpoint || {};
					const se = nav.searchEndpoint || {};
					const params = se.params || '';
					const status = fr.status || '';
					if (!result[title]) result[title] = {};
					result[title][label] = {sp: params, status: status};
				}
			}
			return JSON.stringify(result);
		} catch(e) { return JSON.stringify({error: e.message}); }
	}`
	filterResult, err := page.Eval(combinedFilterExpr)
	if err != nil {
		t.Logf("Filter extraction error: %v", err)
	} else {
		raw := filterResult.Value.Str()
		t.Logf("Filters with Videos applied: %s", raw)
		var parsed interface{}
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			pretty, _ := json.MarshalIndent(parsed, "", "  ")
			fpath := filepath.Join(dumpDir, "filters_with_videos_applied.json")
			os.WriteFile(fpath, pretty, 0o644)
		}

		// Extract "This week" sp value (which now includes Videos filter).
		var filterMap map[string]map[string]struct {
			SP     string `json:"sp"`
			Status string `json:"status"`
		}
		if json.Unmarshal([]byte(raw), &filterMap) == nil {
			if uploadDate, ok := filterMap["Upload date"]; ok {
				if thisWeek, ok := uploadDate["This week"]; ok && thisWeek.SP != "" {
					t.Logf("Combined sp for Videos+This week: %s", thisWeek.SP)
					combinedURL := fmt.Sprintf("https://www.youtube.com/results?search_query=%%E5%%A4%%A9%%E9%%BD%%90%%E9%%94%%82%%E4%%B8%%9A&sp=%s", thisWeek.SP)
					t.Logf("Step 3: Navigating with combined filter: %s", combinedURL)

					if err := page.Navigate(combinedURL); err != nil {
						t.Logf("WARN: navigate combined: %v", err)
					}
					if err := page.WaitLoad(); err != nil {
						t.Logf("WARN: wait load: %v", err)
					}
					time.Sleep(8 * time.Second)
				}
			}
		}
	}

	// Check initial state.
	t.Log("\n========== Checking initial state ==========")
	initialStateExpr := `() => {
		try {
			const data = window.ytInitialData;
			if (!data) return JSON.stringify({error: "no ytInitialData"});
			const result = {};
			const twoCol = data.contents.twoColumnSearchResultsRenderer;
			if (twoCol && twoCol.primaryContents) {
				const section = twoCol.primaryContents.sectionListRenderer;
				if (section && section.contents) {
					result.sectionCount = section.contents.length;
					result.sectionTypes = section.contents.map(c => Object.keys(c)[0]);
					const firstSection = section.contents[0];
					if (firstSection && firstSection.itemSectionRenderer) {
						const items = firstSection.itemSectionRenderer.contents;
						result.itemCount = items ? items.length : 0;
						result.itemTypes = items ? items.map(i => Object.keys(i)[0]) : [];
					}
					result.hasContinuation = false;
					for (const content of section.contents) {
						if (content.continuationItemRenderer) {
							result.hasContinuation = true;
						}
					}
				}
			}
			result.estimatedResults = data.estimatedResults;
			return JSON.stringify(result);
		} catch(e) { return JSON.stringify({error: e.message}); }
	}`
	initialResult, err := page.Eval(initialStateExpr)
	if err != nil {
		t.Logf("Initial state error: %v", err)
	} else {
		raw := initialResult.Value.Str()
		var parsed interface{}
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			pretty, _ := json.MarshalIndent(parsed, "", "  ")
			t.Logf("Initial state:\n%s", string(pretty))
			fpath := filepath.Join(dumpDir, "initial_state.json")
			os.WriteFile(fpath, pretty, 0o644)
		}
	}

	// Save full ytInitialData.
	fullExpr := `() => { try { if (!window.ytInitialData) return ''; return JSON.stringify(window.ytInitialData); } catch(e) { return 'ERROR: ' + e.message; } }`
	fullResult, err := page.Eval(fullExpr)
	if err == nil {
		str := fullResult.Value.Str()
		if str != "" && !strings.HasPrefix(str, "ERROR:") {
			fpath := filepath.Join(dumpDir, "ssr_ytInitialData_filtered_FULL.json")
			os.WriteFile(fpath, []byte(str), 0o644)
			t.Logf("Full ytInitialData saved: %d bytes", len(str))
		}
	}

	// ========== Scroll loop: detect bottom ==========
	t.Log("\n========== Scroll loop: detecting bottom ==========")

	for scrollIdx := 0; scrollIdx < 10; scrollIdx++ {
		scrollPhase = fmt.Sprintf("scroll_%d", scrollIdx+1)

		beforeCountExpr := `() => {
			return JSON.stringify({
				videoRenderers: document.querySelectorAll('ytd-video-renderer').length,
				continuationItems: document.querySelectorAll('ytd-continuation-item-renderer').length,
				messageRenderers: document.querySelectorAll('ytd-message-renderer').length,
			});
		}`
		beforeResult, _ := page.Eval(beforeCountExpr)
		t.Logf("[Scroll %d] Before: %s", scrollIdx+1, beforeResult.Value.Str())

		_, err := page.Eval(`() => { window.scrollBy(0, 3000); }`)
		if err != nil {
			t.Logf("[Scroll %d] scroll error: %v", scrollIdx+1, err)
			break
		}
		time.Sleep(3 * time.Second)

		afterResult, _ := page.Eval(beforeCountExpr)
		t.Logf("[Scroll %d] After: %s", scrollIdx+1, afterResult.Value.Str())

		// Check for end indicators.
		endCheckExpr := `() => {
			const result = {
				noMoreResults: false,
				messageTexts: [],
				continuationVisible: false,
			};
			const msgs = document.querySelectorAll('ytd-message-renderer');
			for (const msg of msgs) {
				const text = msg.textContent.trim();
				if (text) {
					result.messageTexts.push(text);
					if (text.includes('No more results') || text.includes('没有更多结果')) {
						result.noMoreResults = true;
					}
				}
			}
			const contItems = document.querySelectorAll('ytd-continuation-item-renderer');
			for (const ci of contItems) {
				if (ci.offsetHeight > 0) result.continuationVisible = true;
			}
			return JSON.stringify(result);
		}`
		endResult, _ := page.Eval(endCheckExpr)
		t.Logf("[Scroll %d] End check: %s", scrollIdx+1, endResult.Value.Str())

		var endData struct {
			NoMoreResults       bool     `json:"noMoreResults"`
			MessageTexts        []string `json:"messageTexts"`
			ContinuationVisible bool     `json:"continuationVisible"`
		}
		if json.Unmarshal([]byte(endResult.Value.Str()), &endData) == nil {
			if endData.NoMoreResults {
				t.Logf("[Scroll %d] ✅ FOUND 'No more results'! Stopping.", scrollIdx+1)
				break
			}
			if !endData.ContinuationVisible && len(endData.MessageTexts) > 0 {
				t.Logf("[Scroll %d] ✅ No continuation + has messages. Likely at bottom.", scrollIdx+1)
				break
			}
		}

		state := map[string]interface{}{
			"before":   json.RawMessage(beforeResult.Value.Str()),
			"after":    json.RawMessage(afterResult.Value.Str()),
			"endCheck": json.RawMessage(endResult.Value.Str()),
		}
		stateJSON, _ := json.MarshalIndent(state, "", "  ")
		fpath := filepath.Join(dumpDir, fmt.Sprintf("scroll_%d_state.json", scrollIdx+1))
		os.WriteFile(fpath, stateJSON, 0o644)
	}

	// Final summary.
	t.Log("\n========== SCROLL BOTTOM DETECTION SUMMARY ==========")
	finalExpr := `() => {
		return JSON.stringify({
			totalVideoRenderers: document.querySelectorAll('ytd-video-renderer').length,
			totalMessageRenderers: document.querySelectorAll('ytd-message-renderer').length,
			totalContinuationItems: document.querySelectorAll('ytd-continuation-item-renderer').length,
			allMessageTexts: Array.from(document.querySelectorAll('ytd-message-renderer')).map(el => el.textContent.trim()),
			pageHeight: document.documentElement.scrollHeight,
			scrollPosition: window.scrollY,
		});
	}`
	finalResult, _ := page.Eval(finalExpr)
	t.Logf("Final state: %s", finalResult.Value.Str())

	var parsed interface{}
	if json.Unmarshal([]byte(finalResult.Value.Str()), &parsed) == nil {
		pretty, _ := json.MarshalIndent(parsed, "", "  ")
		fpath := filepath.Join(dumpDir, "final_state.json")
		os.WriteFile(fpath, pretty, 0o644)
	}

	capJSON, _ := json.MarshalIndent(apiCaptures, "", "  ")
	os.WriteFile(filepath.Join(dumpDir, "_api_captures.json"), capJSON, 0o644)
	t.Logf("Total API captures: %d", len(apiCaptures))
}

// TestProbeYouTubeAboutPanel verifies how to fetch the "About" panel data
// (join date, total views, country, links) from a YouTube channel.
//
// Run with: go test -v -run TestProbeYouTubeAboutPanel -timeout 120s ./probe/
func TestProbeYouTubeAboutPanel(t *testing.T) {
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
		BrowserBin:  resolveBrowserBin(cfg.Browser.Bin),
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer mgr.Close()

	page := mgr.GetPage()
	defer mgr.PutPage(page)

	dumpDir := filepath.Join("..", "data", "probe", "youtube_about_panel")
	os.MkdirAll(dumpDir, 0o755)

	_ = proto.NetworkEnable{}.Call(page)

	var browseResponses []struct {
		URL  string `json:"url"`
		Body string `json:"body"`
	}

	go page.EachEvent(func(e *proto.NetworkResponseReceived) bool {
		url := e.Response.URL
		if strings.Contains(url, "youtubei/v1/browse") {
			body, err := proto.NetworkGetResponseBody{RequestID: e.RequestID}.Call(page)
			if err == nil && body != nil {
				browseResponses = append(browseResponses, struct {
					URL  string `json:"url"`
					Body string `json:"body"`
				}{URL: url, Body: body.Body})
				safeName := fmt.Sprintf("browse_response_%d.json", len(browseResponses))
				fpath := filepath.Join(dumpDir, safeName)
				os.WriteFile(fpath, []byte(body.Body), 0o644)
				t.Logf("[API] browse response: %d bytes → %s", len(body.Body), safeName)
			}
		}
		return false
	})()

	channelURL := "https://www.youtube.com/@BrunoMars"
	t.Logf("Navigating to %s", channelURL)

	if err := page.Navigate(channelURL); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Logf("WARN: wait load: %v", err)
	}
	time.Sleep(8 * time.Second)

	// Step 1: Extract engagementPanel data from SSR.
	t.Log("\n========== Step 1: Extract engagementPanel from SSR ==========")

	engagementExpr := `() => {
		try {
			const data = window.ytInitialData;
			if (!data) return JSON.stringify({error: "no ytInitialData"});
			const result = {};

			if (data.engagementPanels) {
				result.panelCount = data.engagementPanels.length;
				result.panels = data.engagementPanels.map((panel, idx) => {
					const p = panel.engagementPanelSectionListRenderer;
					if (!p) return {index: idx, type: Object.keys(panel)[0]};
					return {
						index: idx,
						panelIdentifier: p.panelIdentifier || '',
						targetId: p.targetId || '',
						contentKeys: p.content ? Object.keys(p.content) : [],
						headerTitle: p.header && p.header.engagementPanelTitleHeaderRenderer ?
							(p.header.engagementPanelTitleHeaderRenderer.title.simpleText ||
							 (p.header.engagementPanelTitleHeaderRenderer.title.runs ?
							  p.header.engagementPanelTitleHeaderRenderer.title.runs.map(r => r.text).join('') : '')) : '',
					};
				});
			}

			// Look for aboutChannelRenderer in engagement panels.
			if (data.engagementPanels) {
				for (const panel of data.engagementPanels) {
					const p = panel.engagementPanelSectionListRenderer;
					if (!p || !p.content) continue;

					if (p.content.sectionListRenderer) {
						const sections = p.content.sectionListRenderer.contents;
						if (sections) {
							for (const section of sections) {
								if (section.itemSectionRenderer && section.itemSectionRenderer.contents) {
									for (const item of section.itemSectionRenderer.contents) {
										if (item.aboutChannelRenderer) {
											result.aboutChannelFound = true;
											const about = item.aboutChannelRenderer;
											result.aboutChannelKeys = Object.keys(about);
											if (about.metadata) {
												const meta = about.metadata.aboutChannelViewModel;
												if (meta) {
													result.aboutChannelViewModelKeys = Object.keys(meta);
													result.aboutData = {
														description: meta.description ? meta.description.substring(0, 200) : '',
														subscriberCountText: meta.subscriberCountText || '',
														videoCountText: meta.videoCountText || '',
														viewCountText: meta.viewCountText || '',
														joinedDateText: meta.joinedDateText ? JSON.stringify(meta.joinedDateText) : '',
														country: meta.country || '',
														canonicalChannelUrl: meta.canonicalChannelUrl || '',
														channelId: meta.channelId || '',
														links: meta.links ? meta.links.map(link => {
															if (link.channelExternalLinkViewModel) {
																return {
																	title: link.channelExternalLinkViewModel.title ? link.channelExternalLinkViewModel.title.content : '',
																	link: link.channelExternalLinkViewModel.link ? link.channelExternalLinkViewModel.link.content : '',
																};
															}
															return Object.keys(link)[0];
														}) : [],
													};
												}
											}
										}
									}
								}
							}
						}
					}

					if (p.content.continuationItemRenderer) {
						result.hasContinuation = true;
						const ci = p.content.continuationItemRenderer;
						if (ci.continuationEndpoint) {
							result.continuationToken = ci.continuationEndpoint.continuationCommand ?
								ci.continuationEndpoint.continuationCommand.token : '';
						}
					}
				}
			}

			return JSON.stringify(result);
		} catch(e) { return JSON.stringify({error: e.message, stack: e.stack}); }
	}`

	engagementResult, err := page.Eval(engagementExpr)
	if err != nil {
		t.Logf("Engagement panel extraction error: %v", err)
	} else {
		raw := engagementResult.Value.Str()
		var parsed interface{}
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			pretty, _ := json.MarshalIndent(parsed, "", "  ")
			t.Logf("Engagement panel data:\n%s", string(pretty))
			fpath := filepath.Join(dumpDir, "engagement_panel_analysis.json")
			os.WriteFile(fpath, pretty, 0o644)
		}
	}

	// Step 2: Click "...more" to trigger the about panel.
	t.Log("\n========== Step 2: Click '...more' to trigger about panel ==========")

	clickMoreExpr := `() => {
		// Try clicking the truncation text in the description preview.
		const truncLinks = document.querySelectorAll('.truncation-text');
		for (const link of truncLinks) {
			if (link.textContent.trim().includes('more')) {
				link.click();
				return JSON.stringify({clicked: true, method: 'truncation-text', text: link.textContent.trim()});
			}
		}
		// Try the description preview view model.
		const descPreviews = document.querySelectorAll('yt-description-preview-view-model');
		for (const dp of descPreviews) {
			dp.click();
			return JSON.stringify({clicked: true, method: 'description-preview'});
		}
		return JSON.stringify({clicked: false});
	}`

	clickResult, err := page.Eval(clickMoreExpr)
	if err != nil {
		t.Logf("Click more error: %v", err)
	} else {
		t.Logf("Click more result: %s", clickResult.Value.Str())
	}

	time.Sleep(5 * time.Second)

	// Step 3: Extract about panel data from DOM.
	t.Log("\n========== Step 3: Extract about panel data from DOM ==========")

	aboutDomExpr := `() => {
		try {
			const result = {};
			const aboutVM = document.querySelector('yt-about-channel-view-model');
			if (aboutVM) {
				result.aboutViewModelFound = true;
				const allText = aboutVM.textContent.trim();
				result.fullText = allText.substring(0, 1000);

				const links = aboutVM.querySelectorAll('a[href]');
				result.links = Array.from(links).slice(0, 20).map(a => ({
					href: a.href,
					text: a.textContent.trim().substring(0, 100),
				}));

				const allEls = aboutVM.querySelectorAll('*');
				const dataPoints = [];
				for (const el of allEls) {
					if (el.children.length === 0 || el.tagName === 'SPAN' || el.tagName === 'YT-ATTRIBUTED-STRING') {
						const text = el.textContent.trim();
						if (text && text.length > 0 && text.length < 200) {
							if (text.includes('Joined') || text.includes('views') || text.includes('subscriber') ||
								text.includes('video') || text.includes('United') || text.includes('States') ||
								text.match(/\d{4}$/) || text.match(/[A-Z][a-z]+ \d+, \d{4}/)) {
								dataPoints.push({tag: el.tagName, text: text});
							}
						}
					}
				}
				result.dataPoints = dataPoints.slice(0, 30);
			} else {
				result.aboutViewModelFound = false;
				const panels = document.querySelectorAll('ytd-engagement-panel-section-list-renderer');
				result.panelCount = panels.length;
				for (const panel of panels) {
					const title = panel.querySelector('#title-text');
					if (title && title.textContent.trim().toLowerCase().includes('about')) {
						result.aboutPanelInDOM = true;
						result.aboutPanelText = panel.textContent.trim().substring(0, 500);
					}
				}
			}
			return JSON.stringify(result);
		} catch(e) { return JSON.stringify({error: e.message}); }
	}`

	aboutDomResult, err := page.Eval(aboutDomExpr)
	if err != nil {
		t.Logf("About DOM error: %v", err)
	} else {
		raw := aboutDomResult.Value.Str()
		var parsed interface{}
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			pretty, _ := json.MarshalIndent(parsed, "", "  ")
			t.Logf("About panel DOM:\n%s", string(pretty))
			fpath := filepath.Join(dumpDir, "about_panel_dom.json")
			os.WriteFile(fpath, pretty, 0o644)
		}
	}

	// Step 4: Check browse API responses.
	t.Log("\n========== Step 4: Analyze browse API responses ==========")
	t.Logf("Total browse API responses: %d", len(browseResponses))

	for i, resp := range browseResponses {
		var browseData map[string]interface{}
		if json.Unmarshal([]byte(resp.Body), &browseData) == nil {
			keys := make([]string, 0)
			for k := range browseData {
				keys = append(keys, k)
			}
			t.Logf("Browse response %d: %d bytes, keys: %v", i+1, len(resp.Body), keys)
		}
	}

	t.Log("\n========== ABOUT PANEL PROBE COMPLETE ==========")
}

// TestProbeYouTubeShortsTab verifies the data structure of the Shorts tab
// and compares it with the Videos tab structure.
//
// Run with: go test -v -run TestProbeYouTubeShortsTab -timeout 120s ./probe/
func TestProbeYouTubeShortsTab(t *testing.T) {
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
		BrowserBin:  resolveBrowserBin(cfg.Browser.Bin),
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer mgr.Close()

	page := mgr.GetPage()
	defer mgr.PutPage(page)

	dumpDir := filepath.Join("..", "data", "probe", "youtube_shorts_tab")
	os.MkdirAll(dumpDir, 0o755)

	shortsURL := "https://www.youtube.com/@BrunoMars/shorts"
	t.Logf("Navigating to %s", shortsURL)

	if err := page.Navigate(shortsURL); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Logf("WARN: wait load: %v", err)
	}
	time.Sleep(8 * time.Second)

	// Dump full ytInitialData.
	fullExpr := `() => { try { if (!window.ytInitialData) return ''; return JSON.stringify(window.ytInitialData); } catch(e) { return 'ERROR: ' + e.message; } }`
	fullResult, err := page.Eval(fullExpr)
	if err == nil {
		str := fullResult.Value.Str()
		if str != "" && !strings.HasPrefix(str, "ERROR:") {
			fpath := filepath.Join(dumpDir, "ssr_ytInitialData_shorts_FULL.json")
			os.WriteFile(fpath, []byte(str), 0o644)
			t.Logf("Full ytInitialData saved: %d bytes", len(str))
		}
	}

	// Analyze Shorts tab structure.
	shortsAnalyzeExpr := `() => {
		try {
			const data = window.ytInitialData;
			if (!data) return JSON.stringify({exists: false});
			const result = {exists: true};

			if (data.contents && data.contents.twoColumnBrowseResultsRenderer) {
				const tabs = data.contents.twoColumnBrowseResultsRenderer.tabs;
				for (const tab of (tabs || [])) {
					if (tab.tabRenderer && tab.tabRenderer.selected) {
						result.selectedTab = tab.tabRenderer.title;
						const content = tab.tabRenderer.content;
						if (content) {
							result.contentKeys = Object.keys(content);
							const grid = content.richGridRenderer;
							if (grid) {
								result.hasRichGridRenderer = true;
								result.gridContentCount = grid.contents ? grid.contents.length : 0;

								if (grid.header) {
									result.headerKeys = Object.keys(grid.header);
									const chipBar = grid.header.chipBarViewModel;
									if (chipBar && chipBar.chips) {
										result.sortChips = chipBar.chips.map(chip => {
											const vm = chip.chipViewModel;
											return vm ? {text: vm.text, selected: vm.selected} : Object.keys(chip)[0];
										});
									}
								}

								if (grid.contents && grid.contents.length > 0) {
									result.gridItemTypes = grid.contents.slice(0, 5).map(item => Object.keys(item)[0]);

									for (const item of grid.contents) {
										const ri = item.richItemRenderer;
										if (ri && ri.content) {
											const contentKeys = Object.keys(ri.content);
											result.richItemContentType = contentKeys[0];

											if (ri.content.shortsLockupViewModel) {
												const slvm = ri.content.shortsLockupViewModel;
												result.sampleShort = {
													type: 'shortsLockupViewModel',
													allKeys: Object.keys(slvm),
													entityId: slvm.entityId || '',
													accessibilityText: slvm.accessibilityText || '',
													overlayMetadata: slvm.overlayMetadata ? JSON.stringify(slvm.overlayMetadata).substring(0, 500) : '',
													thumbnail: slvm.thumbnail ? JSON.stringify(slvm.thumbnail).substring(0, 300) : '',
													inlinePlayerData: slvm.inlinePlayerData ? 'present' : 'absent',
												};
											} else if (ri.content.reelItemRenderer) {
												const reel = ri.content.reelItemRenderer;
												result.sampleShort = {
													type: 'reelItemRenderer',
													allKeys: Object.keys(reel),
													videoId: reel.videoId || '',
													headline: reel.headline ? (reel.headline.simpleText || JSON.stringify(reel.headline)) : '',
													viewCountText: reel.viewCountText ? (reel.viewCountText.simpleText || JSON.stringify(reel.viewCountText)) : '',
												};
											} else if (ri.content.videoRenderer) {
												const vr = ri.content.videoRenderer;
												result.sampleShort = {
													type: 'videoRenderer',
													allKeys: Object.keys(vr),
													videoId: vr.videoId || '',
													title: vr.title && vr.title.runs ? vr.title.runs.map(r => r.text).join('') : '',
												};
											} else {
												result.sampleShort = {type: contentKeys[0], keys: Object.keys(ri.content[contentKeys[0]])};
											}
											break;
										}
									}

									const lastItem = grid.contents[grid.contents.length - 1];
									if (lastItem && lastItem.continuationItemRenderer) {
										result.hasContinuation = true;
									}
								}
							}
						}
						break;
					}
				}
			}

			return JSON.stringify(result);
		} catch(e) { return JSON.stringify({error: e.message, stack: e.stack}); }
	}`

	shortsResult, err := page.Eval(shortsAnalyzeExpr)
	if err != nil {
		t.Logf("Shorts analysis error: %v", err)
	} else {
		raw := shortsResult.Value.Str()
		var parsed interface{}
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			pretty, _ := json.MarshalIndent(parsed, "", "  ")
			t.Logf("Shorts tab analysis:\n%s", string(pretty))
			fpath := filepath.Join(dumpDir, "analysis_shorts_tab.json")
			os.WriteFile(fpath, pretty, 0o644)
		}
	}

	// Compare with Videos tab.
	t.Log("\n========== Comparing with Videos tab ==========")
	videosURL := "https://www.youtube.com/@BrunoMars/videos"
	t.Logf("Navigating to %s", videosURL)

	if err := page.Navigate(videosURL); err != nil {
		t.Logf("WARN: navigate to videos: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Logf("WARN: wait load: %v", err)
	}
	time.Sleep(8 * time.Second)

	videosAnalyzeExpr := `() => {
		try {
			const data = window.ytInitialData;
			if (!data) return JSON.stringify({exists: false});
			const result = {exists: true};
			if (data.contents && data.contents.twoColumnBrowseResultsRenderer) {
				const tabs = data.contents.twoColumnBrowseResultsRenderer.tabs;
				for (const tab of (tabs || [])) {
					if (tab.tabRenderer && tab.tabRenderer.selected) {
						result.selectedTab = tab.tabRenderer.title;
						const content = tab.tabRenderer.content;
						if (content) {
							result.contentKeys = Object.keys(content);
							const grid = content.richGridRenderer;
							if (grid) {
								result.hasRichGridRenderer = true;
								result.gridContentCount = grid.contents ? grid.contents.length : 0;
								if (grid.header) {
									result.headerKeys = Object.keys(grid.header);
									const chipBar = grid.header.chipBarViewModel;
									if (chipBar && chipBar.chips) {
										result.sortChips = chipBar.chips.map(chip => {
											const vm = chip.chipViewModel;
											return vm ? {text: vm.text, selected: vm.selected} : Object.keys(chip)[0];
										});
									}
								}
								if (grid.contents && grid.contents.length > 0) {
									result.gridItemTypes = grid.contents.slice(0, 5).map(item => Object.keys(item)[0]);
									for (const item of grid.contents) {
										const ri = item.richItemRenderer;
										if (ri && ri.content) {
											result.richItemContentType = Object.keys(ri.content)[0];
											if (ri.content.videoRenderer) {
												const vr = ri.content.videoRenderer;
												result.sampleVideo = {
													type: 'videoRenderer',
													allKeys: Object.keys(vr),
													videoId: vr.videoId || '',
													title: vr.title && vr.title.runs ? vr.title.runs.map(r => r.text).join('') : '',
													viewCountText: vr.viewCountText ? (vr.viewCountText.simpleText || '') : '',
													publishedTimeText: vr.publishedTimeText ? vr.publishedTimeText.simpleText : '',
													lengthText: vr.lengthText ? vr.lengthText.simpleText : '',
												};
											}
											break;
										}
									}
									const lastItem = grid.contents[grid.contents.length - 1];
									if (lastItem && lastItem.continuationItemRenderer) result.hasContinuation = true;
								}
							}
						}
						break;
					}
				}
			}
			return JSON.stringify(result);
		} catch(e) { return JSON.stringify({error: e.message}); }
	}`

	videosResult, err := page.Eval(videosAnalyzeExpr)
	if err != nil {
		t.Logf("Videos analysis error: %v", err)
	} else {
		raw := videosResult.Value.Str()
		var parsed interface{}
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			pretty, _ := json.MarshalIndent(parsed, "", "  ")
			t.Logf("Videos tab (comparison):\n%s", string(pretty))
			fpath := filepath.Join(dumpDir, "analysis_videos_tab_comparison.json")
			os.WriteFile(fpath, pretty, 0o644)
		}
	}

	t.Log("\n========== SHORTS TAB PROBE COMPLETE ==========")
}
