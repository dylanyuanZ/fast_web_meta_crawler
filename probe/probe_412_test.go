package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/platform/bilibili"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/pool"
)

// ============================================================================
// 412 Anti-Crawl Mechanism Probe Tests (Realistic Workload Version)
//
// Purpose: Reproduce the 412 rate-limiting that occurs during real crawler runs.
//
// Key insight: The old probe couldn't trigger 412 because it started from a
// "cold" session. In real crawler runs, Stage 0 (search pages) accumulates
// a large request debt, and Stage 1 (author pages) immediately triggers 412
// because the rate-limit window hasn't reset.
//
// This version simulates the real crawler's two-stage workflow:
//   Phase 1 (Stage 0 simulation): NavigateAndExtract on search pages
//   Phase 2 (Stage 1 simulation): NavigateAndIntercept on author video pages
//
// Run: go test -v -run TestProbe412_RealisticWorkload -timeout 600s ./probe/
// ============================================================================

// probeConfig holds shared config for 412 probe experiments.
type probeConfig struct {
	cfg        *config.Config
	browserCfg browser.Config
}

func newProbeConfig(t *testing.T) *probeConfig {
	t.Helper()
	if err := config.Load("../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()
	return &probeConfig{
		cfg: cfg,
		browserCfg: browser.Config{
			Headless:    cfg.Browser.IsHeadless(),
			UserDataDir: cfg.Browser.UserDataDir,
			Concurrency: cfg.Concurrency, // Use real concurrency from config (3).
			BrowserBin:  cfg.Browser.Bin,
		},
	}
}

func (pc *probeConfig) createBrowser(t *testing.T) *browser.Manager {
	t.Helper()
	mgr, err := browser.New(pc.browserCfg)
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := browser.EnsureLogin(ctx, mgr, "https://www.bilibili.com", pc.cfg.Cookie, bilibili.BilibiliLoginChecker); err != nil {
		mgr.Close()
		t.Fatalf("ensure login: %v", err)
	}
	return mgr
}

// ============================================================================
// Phase 1: Simulate Stage 0 — search page requests via NavigateAndExtract
// ============================================================================

// simulateStage0 mimics the real Stage 0 workflow: concurrent workers navigate
// to search pages and extract SSR data via NavigateAndExtract, exactly like
// RunStage0() does with its Worker Pool.
//
// CRITICAL: The real crawler uses concurrent workers (concurrency=3) for Stage 0,
// NOT serial requests. This means 3 search pages are fetched simultaneously,
// creating 3x the request density. The old serial probe missed this, which is
// why it couldn't trigger 412.
func simulateStage0(t *testing.T, ctx context.Context, mgr *browser.Manager, keyword string, maxPages int, concurrency int, interval time.Duration) (successPages int, authorMids []string) {
	t.Helper()

	seen := make(map[string]bool)
	var mu sync.Mutex
	var successCount int32

	// Build page tasks: pages 2..maxPages (page 1 is fetched first, same as real crawler).
	// Real crawler fetches page 1 first to get totalPages, then dispatches remaining pages
	// to the worker pool.

	// Step 1: Fetch page 1 serially (same as real crawler).
	t.Log("  [Stage0] Fetching page 1 (serial, same as real crawler)...")
	p := mgr.GetPage()
	targetURL := fmt.Sprintf("https://search.bilibili.com/video?keyword=%s&page=%d", url.QueryEscape(keyword), 1)
	rawJSON, err := browser.NavigateAndExtract(ctx, p, targetURL, bilibili.SSRExtractJS)
	mgr.PutPage(p)

	if err != nil {
		t.Logf("  [Stage0] Page 1 FAILED: %v", err)
	} else {
		var data bilibili.SearchData
		if err := json.Unmarshal([]byte(rawJSON), &data); err == nil {
			for _, item := range data.Result {
				midStr := fmt.Sprintf("%d", item.Mid)
				if !seen[midStr] {
					seen[midStr] = true
					authorMids = append(authorMids, midStr)
				}
			}
		}
		atomic.AddInt32(&successCount, 1)
	}

	// Step 2: Dispatch remaining pages to concurrent workers (same as real crawler's pool.Run).
	var (
		idx int32 // next page index to process
		wg  sync.WaitGroup
	)

	remainingPages := maxPages - 1 // pages 2..maxPages
	t.Logf("  [Stage0] Dispatching pages 2-%d to %d concurrent workers...", maxPages, concurrency)

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				i := int(atomic.AddInt32(&idx, 1) - 1)
				if i >= remainingPages {
					return
				}
				if ctx.Err() != nil {
					return
				}

				page := i + 2 // pages start from 2

				// Use the same request interval as the real crawler.
				if i > 0 {
					time.Sleep(pool.JitteredDuration(interval))
				}

				// Exactly replicate SearchPage(): NavigateAndExtract + SSRExtractJS.
				p := mgr.GetPage()
				targetURL := fmt.Sprintf("https://search.bilibili.com/video?keyword=%s&page=%d", url.QueryEscape(keyword), page)

				rawJSON, err := browser.NavigateAndExtract(ctx, p, targetURL, bilibili.SSRExtractJS)
				mgr.PutPage(p)

				if err != nil {
					t.Logf("  [Stage0][w%d] Page %d FAILED: %v", workerID, page, err)
					continue
				}

				// Parse to extract author mids.
				var data bilibili.SearchData
				if err := json.Unmarshal([]byte(rawJSON), &data); err != nil {
					t.Logf("  [Stage0][w%d] Page %d parse error: %v", workerID, page, err)
					continue
				}

				mu.Lock()
				for _, item := range data.Result {
					midStr := fmt.Sprintf("%d", item.Mid)
					if !seen[midStr] {
						seen[midStr] = true
						authorMids = append(authorMids, midStr)
					}
				}
				mu.Unlock()

				newCount := atomic.AddInt32(&successCount, 1)
				if newCount%5 == 0 {
					mu.Lock()
					t.Logf("  [Stage0] %d/%d pages done, %d unique authors so far", newCount, maxPages, len(authorMids))
					mu.Unlock()
				}
			}
		}(w)
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	successPages = int(atomic.LoadInt32(&successCount))
	t.Logf("  [Stage0] Completed: %d/%d pages, %d unique authors", successPages, maxPages, len(authorMids))
	return successPages, authorMids
}

// ============================================================================
// Phase 2: Simulate Stage 1 — author video page requests via NavigateAndIntercept
// ============================================================================

// stage1Result records the outcome of a single author request in Phase 2.
type stage1Result struct {
	mid    string
	status string // "ok", "412", "timeout", "error"
	err    error
}

// simulateStage1 mimics the real Stage 1 workflow: concurrent workers fetch
// author info AND video pages, exactly like processOneAuthorOnce() does.
//
// CRITICAL: The real crawler does TWO navigations per author:
//  1. FetchAuthorInfo:       NavigateAndIntercept to space.bilibili.com/{mid}
//     (intercepts /x/space/wbi/acc/info + /x/relation/stat)
//  2. FetchAllAuthorVideos:  NavigateAndIntercept to space.bilibili.com/{mid}/video
//     (intercepts /x/space/wbi/arc/search)
//
// The old probe only did step 2, missing half the request load.
func simulateStage1(t *testing.T, ctx context.Context, mgr *browser.Manager, mids []string, concurrency int, interval time.Duration) []stage1Result {
	t.Helper()

	// Rules for FetchAuthorInfo (step 1).
	infoRules := []browser.InterceptRule{
		{URLPattern: "/x/space/wbi/acc/info", ID: "user_info"},
		{URLPattern: "/x/relation/stat", ID: "user_stat"},
	}

	// Rules for FetchAllAuthorVideos (step 2).
	videoRules := []browser.InterceptRule{{
		URLPattern: "/x/space/wbi/arc/search",
		ID:         "video_list",
	}}

	var (
		results []stage1Result
		mu      sync.Mutex
		idx     int32
		wg      sync.WaitGroup
	)

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				i := int(atomic.AddInt32(&idx, 1) - 1)
				if i >= len(mids) {
					return
				}
				if ctx.Err() != nil {
					return
				}

				mid := mids[i]

				// Use the same request interval as the real crawler.
				if i >= concurrency { // Skip interval for the first batch.
					time.Sleep(pool.JitteredDuration(interval))
				}

				r := stage1Result{mid: mid}

				// IMPORTANT: Create a per-author timeout context (30s) to match
				// the real crawler's defaultInterceptTimeout. Without this, if an
				// author has no videos (no arc/search API triggered), the intercept
				// would wait for the global 8-min ctx, hanging the entire test.
				authorCtx, authorCancel := context.WithTimeout(ctx, 30*time.Second)

				// Step 1: FetchAuthorInfo — NavigateAndIntercept to space page.
				// Exactly replicates FetchAuthorInfo() in author.go.
				infoPage := mgr.GetPage()
				infoURL := fmt.Sprintf("https://space.bilibili.com/%s", mid)
				_, infoErr := browser.NavigateAndIntercept(authorCtx, infoPage, infoURL, infoRules)
				mgr.PutPage(infoPage)

				if infoErr != nil {
					authorCancel()
					errMsg := infoErr.Error()
					if probe412Contains412(errMsg) {
						r.status = "412"
					} else if probe412ContainsTimeout(errMsg) {
						r.status = "timeout"
					} else {
						r.status = "error"
					}
					r.err = fmt.Errorf("fetch author info: %w", infoErr)

					mu.Lock()
					results = append(results, r)
					mu.Unlock()
					t.Logf("  [Stage1][w%d] mid=%s → %s (info step)", workerID, mid, r.status)
					if r.err != nil {
						t.Logf("    error: %v", r.err)
					}
					continue
				}

				// Brief pause between API calls with jitter (same as real crawler).
				time.Sleep(pool.JitteredDuration(interval))

				// Step 2: FetchAllAuthorVideos — NavigateAndIntercept to video page.
				// Exactly replicates FetchAllAuthorVideos() in author.go.
				videoPage := mgr.GetPage()
				videoURL := fmt.Sprintf("https://space.bilibili.com/%s/video", mid)
				interceptResults, videoErr := browser.NavigateAndIntercept(authorCtx, videoPage, videoURL, videoRules)
				mgr.PutPage(videoPage)
				authorCancel() // Release the per-author context.

				if videoErr != nil {
					errMsg := videoErr.Error()
					if probe412Contains412(errMsg) {
						r.status = "412"
					} else if probe412ContainsTimeout(errMsg) {
						r.status = "timeout"
					} else {
						r.status = "error"
					}
					r.err = fmt.Errorf("fetch videos: %w", videoErr)
				} else if len(interceptResults) > 0 {
					// Validate the response like the real crawler does.
					var quickCheck struct {
						Code int `json:"code"`
					}
					if jsonErr := json.Unmarshal(interceptResults[0].Body, &quickCheck); jsonErr != nil || quickCheck.Code != 0 {
						r.status = "api_error"
						r.err = fmt.Errorf("API code=%d", quickCheck.Code)
					} else {
						r.status = "ok"
					}
				} else {
					r.status = "empty"
					r.err = fmt.Errorf("no intercept results")
				}

				mu.Lock()
				results = append(results, r)
				mu.Unlock()

				t.Logf("  [Stage1][w%d] mid=%s → %s", workerID, mid, r.status)
				if r.err != nil && r.status != "ok" {
					t.Logf("    error: %v", r.err)
				}
			}
		}(w)
	}

	wg.Wait()
	return results
}

// probe412Contains412 checks if an error message indicates a 412 response.
func probe412Contains412(s string) bool {
	return len(s) > 0 && (probe412ContainsStr(s, "status=412") || probe412ContainsStr(s, "412"))
}

// probe412ContainsTimeout checks if an error message indicates a timeout.
func probe412ContainsTimeout(s string) bool {
	return len(s) > 0 && (probe412ContainsStr(s, "timeout") || probe412ContainsStr(s, "context deadline exceeded"))
}

// probe412ContainsStr is a simple substring check.
func probe412ContainsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// Main test: Realistic two-stage workload
// ============================================================================

// TestProbe412_RealisticWorkload simulates the real crawler's two-stage workflow
// to reproduce the 412 rate-limiting behavior.
//
// Phase 1 (Stage 0): Fetch N search pages to accumulate request debt.
// Phase 2 (Stage 1): Fetch author video pages concurrently to trigger 412.
//
// Run: go test -v -run TestProbe412_RealisticWorkload -timeout 600s ./probe/
func TestProbe412_RealisticWorkload(t *testing.T) {
	pc := newProbeConfig(t)

	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	mgr := pc.createBrowser(t)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	keyword := "生化危机9"
	searchPages := 24                  // Match real crawler's actual page count (24 pages observed in logs).
	stage1Authors := 20                // Try 20 authors in Stage 1 (each does 2 navigations = 40 total).
	interval := pc.cfg.RequestInterval // Use real interval from config (2500ms).
	concurrency := pc.cfg.Concurrency  // Use real concurrency from config (3).

	// NOTE: By default, use the real config interval. To increase 412 likelihood,
	// uncomment the aggressive mode below.
	// aggressiveInterval := 0 * time.Millisecond
	// t.Logf("NOTE: Using ZERO interval (config was %v) to maximize 412 likelihood", interval)
	// interval = aggressiveInterval

	t.Log("╔══════════════════════════════════════════════════════════════╗")
	t.Log("║  Probe 412: Realistic Two-Stage Workload                   ║")
	t.Log("╠══════════════════════════════════════════════════════════════╣")
	t.Logf("║  Keyword:     %s", keyword)
	t.Logf("║  Search pages: %d", searchPages)
	t.Logf("║  Stage1 authors: %d", stage1Authors)
	t.Logf("║  Interval:    %v", interval)
	t.Logf("║  Concurrency: %d", concurrency)
	t.Log("╚══════════════════════════════════════════════════════════════╝")

	// ========== Phase 1: Simulate Stage 0 ==========
	t.Log("")
	t.Log("━━━ Phase 1: Simulating Stage 0 (search pages) ━━━")
	t.Logf("Fetching %d search pages for keyword %q...", searchPages, keyword)

	phase1Start := time.Now()
	successPages, authorMids := simulateStage0(t, ctx, mgr, keyword, searchPages, concurrency, interval)
	phase1Duration := time.Since(phase1Start)

	t.Logf("Phase 1 done: %d pages in %v, discovered %d authors", successPages, phase1Duration.Round(time.Second), len(authorMids))

	if len(authorMids) == 0 {
		t.Fatal("Phase 1 found no authors, cannot proceed to Phase 2")
	}

	// Cap authors for Phase 2.
	if len(authorMids) > stage1Authors {
		authorMids = authorMids[:stage1Authors]
	}

	// ========== Transition: NO pause between stages (same as real crawler) ==========
	t.Log("")
	t.Log("━━━ Transition: Stage 0 → Stage 1 (NO pause, same as real crawler) ━━━")

	// ========== Phase 2: Simulate Stage 1 ==========
	t.Log("")
	t.Log("━━━ Phase 2: Simulating Stage 1 (author video pages) ━━━")
	t.Logf("Fetching %d authors with concurrency=%d...", len(authorMids), concurrency)

	phase2Start := time.Now()
	results := simulateStage1(t, ctx, mgr, authorMids, concurrency, interval)
	phase2Duration := time.Since(phase2Start)

	// ========== Summary ==========
	t.Log("")
	t.Log("╔══════════════════════════════════════════════════════════════╗")
	t.Log("║                       SUMMARY                              ║")
	t.Log("╠══════════════════════════════════════════════════════════════╣")

	counts := map[string]int{}
	for _, r := range results {
		counts[r.status]++
	}

	t.Logf("║  Phase 1 (Stage 0): %d/%d pages in %v", successPages, searchPages, phase1Duration.Round(time.Second))
	t.Logf("║  Phase 2 (Stage 1): %d authors in %v", len(results), phase2Duration.Round(time.Second))
	t.Logf("║    OK:      %d", counts["ok"])
	t.Logf("║    412:     %d", counts["412"])
	t.Logf("║    Timeout: %d", counts["timeout"])
	t.Logf("║    Error:   %d", counts["error"])
	t.Logf("║    API err: %d", counts["api_error"])
	t.Logf("║    Empty:   %d", counts["empty"])
	t.Log("╠══════════════════════════════════════════════════════════════╣")

	got412 := counts["412"] > 0
	gotTimeout := counts["timeout"] > 0

	if got412 {
		t.Logf("║  ✅ 412 REPRODUCED! (%d out of %d authors)", counts["412"], len(results))
		t.Log("║  The realistic workload successfully triggers B站 rate limiting.")
	} else if gotTimeout {
		t.Logf("║  ⚠️  No explicit 412, but %d timeouts (likely rate-limited)", counts["timeout"])
		t.Log("║  Timeouts during rate limiting often indicate the same anti-crawl mechanism.")
	} else {
		t.Logf("║  ❌ 412 NOT reproduced. All %d authors succeeded.", counts["ok"])
		t.Log("║  Try increasing searchPages or reducing interval.")
	}
	t.Log("╚══════════════════════════════════════════════════════════════╝")
}

func init() {
	// Ensure log output includes timestamps for timing analysis.
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
}
