package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/platform/bilibili"
)

// ============================================================================
// Stage 1 Performance Probe Tests
//
// Purpose: Reproduce and diagnose the 412 slowdown that occurs during Stage 1
// crawling after processing several authors. Unlike probe_412_test.go which
// used a custom singleRequest function, these tests use the REAL production
// code path (FetchAuthorInfo + FetchAllAuthorVideos) to accurately reproduce
// the issue.
//
// Key insight: The previous probe tests (probe_412_test.go) failed to trigger
// 412 because they used a different request pattern than production code.
// Production code navigates to author pages, intercepts multiple APIs, and
// paginates — this creates a different fingerprint than simple navigation.
//
// Test data: Uses the "炸鸡" keyword's author list from progress file.
//
// Run: go test -v -run TestProbeStage1 -timeout 600s ./probe/
// ============================================================================

// stage1ProbeConfig holds shared config for Stage 1 probe experiments.
type stage1ProbeConfig struct {
	cfg        *config.Config
	browserCfg browser.Config
}

func newStage1ProbeConfig(t *testing.T) *stage1ProbeConfig {
	t.Helper()
	if err := config.Load("../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()
	return &stage1ProbeConfig{
		cfg: cfg,
		browserCfg: browser.Config{
			Headless:    cfg.Browser.IsHeadless(),
			UserDataDir: cfg.Browser.UserDataDir,
			Concurrency: 3,
			BrowserBin:  cfg.Browser.Bin,
		},
	}
}

func (pc *stage1ProbeConfig) createBrowser(t *testing.T, concurrency int) *browser.Manager {
	t.Helper()
	cfg := pc.browserCfg
	cfg.Concurrency = concurrency
	mgr, err := browser.New(cfg)
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

// authorResult records the outcome of processing one author.
type authorResult struct {
	Index     int
	MID       string
	Name      string
	Phase     string // "info" or "videos"
	Status    string // "ok", "412", "error"
	Videos    int
	Duration  time.Duration
	Error     string
	Timestamp time.Time
}

// loadTestMids loads author MIDs from the progress file.
// Returns up to `limit` authors. If limit <= 0, returns all.
func loadTestMids(t *testing.T, limit int) []src.AuthorMid {
	t.Helper()

	// Try to find the progress file.
	progressDir := "../data/"
	entries, err := os.ReadDir(progressDir)
	if err != nil {
		t.Fatalf("read data dir: %v", err)
	}

	var progressFile string
	for _, e := range entries {
		if !e.IsDir() && len(e.Name()) > 9 && e.Name()[:9] == "progress_" {
			progressFile = progressDir + e.Name()
			break
		}
	}
	if progressFile == "" {
		t.Fatal("no progress file found in data/")
	}

	data, err := os.ReadFile(progressFile)
	if err != nil {
		t.Fatalf("read progress file: %v", err)
	}

	var progress struct {
		AuthorMids []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"author_mids"`
	}
	if err := json.Unmarshal(data, &progress); err != nil {
		t.Fatalf("parse progress file: %v", err)
	}

	var mids []src.AuthorMid
	for _, m := range progress.AuthorMids {
		mids = append(mids, src.AuthorMid{Name: m.Name, ID: m.ID})
	}

	if limit > 0 && len(mids) > limit {
		mids = mids[:limit]
	}

	t.Logf("Loaded %d authors from %s", len(mids), progressFile)
	return mids
}

// processOneAuthorProbe runs the real production code path for one author
// and returns detailed timing/status information.
func processOneAuthorProbe(ctx context.Context, crawler *bilibili.BiliBrowserAuthorCrawler, mid src.AuthorMid, index int, maxVideos int) authorResult {
	start := time.Now()

	// Phase 1: FetchAuthorInfo
	infoRow, err := crawler.FetchAuthorInfo(ctx, mid.ID)
	if err != nil {
		errStr := err.Error()
		status := "error"
		if contains412(errStr) {
			status = "412"
		}
		return authorResult{
			Index:     index,
			MID:       mid.ID,
			Name:      mid.Name,
			Phase:     "info",
			Status:    status,
			Duration:  time.Since(start),
			Error:     errStr,
			Timestamp: time.Now(),
		}
	}

	// Extract author name from the info row (first column).
	authorName := mid.Name
	if len(infoRow) > 0 {
		authorName = infoRow[0]
	}

	// Brief pause between info and videos (same as production code).
	time.Sleep(500 * time.Millisecond)

	// Phase 2: FetchAllAuthorVideos
	videos, err := crawler.FetchAllAuthorVideos(ctx, mid.ID, maxVideos)
	if err != nil {
		errStr := err.Error()
		status := "error"
		if contains412(errStr) {
			status = "412"
		}
		return authorResult{
			Index:     index,
			MID:       mid.ID,
			Name:      authorName,
			Phase:     "videos",
			Status:    status,
			Videos:    0,
			Duration:  time.Since(start),
			Error:     errStr,
			Timestamp: time.Now(),
		}
	}

	return authorResult{
		Index:     index,
		MID:       mid.ID,
		Name:      authorName,
		Phase:     "complete",
		Status:    "ok",
		Videos:    len(videos),
		Duration:  time.Since(start),
		Timestamp: time.Now(),
	}
}

// contains412 checks if an error string indicates a 412 response.
func contains412(s string) bool {
	return len(s) > 0 && (containsSubstr(s, "412") || containsSubstr(s, "risk control") || containsSubstr(s, "intercept timeout"))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestProbeStage1_Sequential processes authors one by one with a single browser tab.
// This establishes the baseline: how many authors can we process before 412 triggers?
//
// Key metrics:
// - Time per author (info + videos)
// - Which author # triggers the first 412
// - Whether 412 hits info or videos first
//
// Run: go test -v -run TestProbeStage1_Sequential -timeout 600s ./probe/
func TestProbeStage1_Sequential(t *testing.T) {
	pc := newStage1ProbeConfig(t)

	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	testMids := loadTestMids(t, 20)

	mgr := pc.createBrowser(t, 1) // Single tab for sequential processing.
	defer mgr.Close()

	crawler := bilibili.NewAuthorCrawler(mgr)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	t.Log("╔══════════════════════════════════════════════════════════════╗")
	t.Log("║  Experiment: Sequential Stage 1 (1 worker, 1 tab)          ║")
	t.Log("║  Goal: Find baseline 412 trigger point                     ║")
	t.Log("╚══════════════════════════════════════════════════════════════╝")

	maxVideos := 50 // Limit videos per author to keep test manageable.
	var results []authorResult
	first412Index := -1

	for i, mid := range testMids {
		if ctx.Err() != nil {
			t.Logf("Context cancelled at author #%d", i+1)
			break
		}

		t.Logf("--- Author #%d: %s (mid=%s) ---", i+1, mid.Name, mid.ID)
		result := processOneAuthorProbe(ctx, crawler, mid, i+1, maxVideos)
		results = append(results, result)

		switch result.Status {
		case "ok":
			t.Logf("  ✅ OK: %d videos in %v", result.Videos, result.Duration.Round(time.Millisecond))
		case "412":
			t.Logf("  ⚠️ 412 at phase=%s after %v: %s", result.Phase, result.Duration.Round(time.Millisecond), result.Error)
			if first412Index < 0 {
				first412Index = i + 1
			}
		case "error":
			t.Logf("  ❌ ERROR at phase=%s after %v: %s", result.Phase, result.Duration.Round(time.Millisecond), result.Error)
		}

		// If we've seen 3 consecutive 412s, stop — the pattern is clear.
		if len(results) >= 3 {
			last3 := results[len(results)-3:]
			all412 := true
			for _, r := range last3 {
				if r.Status != "412" {
					all412 = false
					break
				}
			}
			if all412 {
				t.Log("⛔ 3 consecutive 412s detected, stopping experiment.")
				break
			}
		}
	}

	// Summary.
	t.Log("\n╔══════════════════════════════════════════════════════════════╗")
	t.Log("║                    SEQUENTIAL SUMMARY                      ║")
	t.Log("╠══════════════════════════════════════════════════════════════╣")

	okCount := 0
	errCount := 0
	count412 := 0
	var totalDuration time.Duration
	for _, r := range results {
		switch r.Status {
		case "ok":
			okCount++
		case "412":
			count412++
		case "error":
			errCount++
		}
		totalDuration += r.Duration
	}

	t.Logf("║  Total authors attempted: %d", len(results))
	t.Logf("║  Success: %d, 412: %d, Error: %d", okCount, count412, errCount)
	t.Logf("║  Total time: %v", totalDuration.Round(time.Second))
	if first412Index > 0 {
		t.Logf("║  First 412 at author #%d", first412Index)
	} else {
		t.Log("║  No 412 triggered! ✅")
	}
	if okCount > 0 {
		t.Logf("║  Avg time per successful author: %v", (totalDuration / time.Duration(okCount)).Round(time.Millisecond))
	}
	t.Log("╚══════════════════════════════════════════════════════════════╝")
}

// TestProbeStage1_Concurrent processes authors with 3 concurrent workers,
// matching the real production configuration. This should reproduce the 412 issue.
//
// Run: go test -v -run TestProbeStage1_Concurrent -timeout 600s ./probe/
func TestProbeStage1_Concurrent(t *testing.T) {
	pc := newStage1ProbeConfig(t)

	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	testMids := loadTestMids(t, 20)

	concurrency := 3
	mgr := pc.createBrowser(t, concurrency)
	defer mgr.Close()

	crawler := bilibili.NewAuthorCrawler(mgr)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	t.Log("╔══════════════════════════════════════════════════════════════╗")
	t.Log("║  Experiment: Concurrent Stage 1 (3 workers, 3 tabs)        ║")
	t.Log("║  Goal: Reproduce 412 under real production conditions      ║")
	t.Log("╚══════════════════════════════════════════════════════════════╝")

	maxVideos := 50
	requestInterval := 2500 * time.Millisecond // Match production config.

	var (
		mu         sync.Mutex
		allResults []authorResult
		taskIdx    int32
		found412   int32
	)

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				// Stop if we've seen enough 412s.
				if atomic.LoadInt32(&found412) >= 3 {
					return
				}

				idx := int(atomic.AddInt32(&taskIdx, 1) - 1)
				if idx >= len(testMids) {
					return
				}

				mid := testMids[idx]
				t.Logf("[w%d] Starting author #%d: %s (mid=%s)", workerID, idx+1, mid.Name, mid.ID)

				result := processOneAuthorProbe(ctx, crawler, mid, idx+1, maxVideos)

				mu.Lock()
				allResults = append(allResults, result)
				mu.Unlock()

				switch result.Status {
				case "ok":
					t.Logf("[w%d] ✅ Author #%d %s: %d videos in %v", workerID, idx+1, result.Name, result.Videos, result.Duration.Round(time.Millisecond))
				case "412":
					atomic.AddInt32(&found412, 1)
					t.Logf("[w%d] ⚠️ Author #%d %s: 412 at phase=%s after %v", workerID, idx+1, result.Name, result.Phase, result.Duration.Round(time.Millisecond))
				case "error":
					t.Logf("[w%d] ❌ Author #%d %s: error at phase=%s: %s", workerID, idx+1, result.Name, result.Phase, result.Error)
				}

				// Request interval between authors (match production).
				select {
				case <-ctx.Done():
					return
				case <-time.After(requestInterval):
				}
			}
		}(w)
	}

	wg.Wait()

	// Summary.
	t.Log("\n╔══════════════════════════════════════════════════════════════╗")
	t.Log("║                   CONCURRENT SUMMARY                       ║")
	t.Log("╠══════════════════════════════════════════════════════════════╣")

	okCount := 0
	errCount := 0
	count412 := 0
	for _, r := range allResults {
		switch r.Status {
		case "ok":
			okCount++
		case "412":
			count412++
		case "error":
			errCount++
		}
	}

	t.Logf("║  Total authors attempted: %d / %d", len(allResults), len(testMids))
	t.Logf("║  Success: %d, 412: %d, Error: %d", okCount, count412, errCount)
	if count412 > 0 {
		t.Log("║  ")
		t.Log("║  412 Details:")
		for _, r := range allResults {
			if r.Status == "412" {
				t.Logf("║    Author #%d %s: phase=%s, time=%v", r.Index, r.Name, r.Phase, r.Timestamp.Format("15:04:05"))
			}
		}
	}
	t.Log("╚══════════════════════════════════════════════════════════════╝")
}

// TestProbeStage1_LargeScale processes 60 authors with 3 concurrent workers
// to reproduce the 412 issue that occurs "after running for a while".
// This is the key experiment — 20 authors wasn't enough to trigger 412.
//
// Run: go test -v -run TestProbeStage1_LargeScale -timeout 1200s ./probe/
func TestProbeStage1_LargeScale(t *testing.T) {
	pc := newStage1ProbeConfig(t)

	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	testMids := loadTestMids(t, 60) // 60 authors should be enough to trigger 412.

	concurrency := 3
	mgr := pc.createBrowser(t, concurrency)
	defer mgr.Close()

	crawler := bilibili.NewAuthorCrawler(mgr)

	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()

	// Write results to a file for async reading (test may exceed tool timeout).
	resultFile, err := os.Create("../log/probe_stage1_results.log")
	if err != nil {
		t.Fatalf("create result file: %v", err)
	}
	defer resultFile.Close()
	writeResult := func(format string, args ...interface{}) {
		msg := fmt.Sprintf(format, args...)
		t.Log(msg)
		fmt.Fprintln(resultFile, msg)
		resultFile.Sync()
	}

	writeResult("╔══════════════════════════════════════════════════════════════╗")
	writeResult("║  Experiment: Large-Scale Concurrent Stage 1                 ║")
	writeResult("║  60 authors, 3 workers, NO request interval (stress test)   ║")
	writeResult("║  Goal: Reproduce 412 and find the exact trigger point       ║")
	writeResult("╚══════════════════════════════════════════════════════════════╝")

	maxVideos := 30 // Reduced to speed up test while still generating pagination.
	// NO requestInterval — maximize pressure to trigger 412 faster.

	var (
		mu         sync.Mutex
		allResults []authorResult
		taskIdx    int32
		found412   int32
		startTime  = time.Now()
	)

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				// Stop after 5 consecutive 412s.
				if atomic.LoadInt32(&found412) >= 5 {
					return
				}

				idx := int(atomic.AddInt32(&taskIdx, 1) - 1)
				if idx >= len(testMids) {
					return
				}

				mid := testMids[idx]
				elapsed := time.Since(startTime).Round(time.Second)
				writeResult("[w%d][T+%v] Starting author #%d: %s (mid=%s)", workerID, elapsed, idx+1, mid.Name, mid.ID)

				result := processOneAuthorProbe(ctx, crawler, mid, idx+1, maxVideos)

				mu.Lock()
				allResults = append(allResults, result)
				mu.Unlock()

				elapsed = time.Since(startTime).Round(time.Second)
				switch result.Status {
				case "ok":
					writeResult("[w%d][T+%v] ✅ #%d %s: %d videos in %v", workerID, elapsed, idx+1, result.Name, result.Videos, result.Duration.Round(time.Millisecond))
				case "412":
					atomic.AddInt32(&found412, 1)
					writeResult("[w%d][T+%v] ⚠️ #%d %s: 412 at phase=%s after %v", workerID, elapsed, idx+1, result.Name, result.Phase, result.Duration.Round(time.Millisecond))
				case "error":
					writeResult("[w%d][T+%v] ❌ #%d %s: error at phase=%s: %s", workerID, elapsed, idx+1, result.Name, result.Phase, result.Error)
				}

				// No requestInterval — stress test to trigger 412 faster.
			}
		}(w)
	}

	wg.Wait()

	// Summary.
	writeResult("\n╔══════════════════════════════════════════════════════════════╗")
	writeResult("║                 LARGE-SCALE SUMMARY                        ║")
	writeResult("╠══════════════════════════════════════════════════════════════╣")

	okCount := 0
	errCount := 0
	count412 := 0
	for _, r := range allResults {
		switch r.Status {
		case "ok":
			okCount++
		case "412":
			count412++
		case "error":
			errCount++
		}
	}

	writeResult("║  Total authors attempted: %d / %d", len(allResults), len(testMids))
	writeResult("║  Success: %d, 412: %d, Error: %d", okCount, count412, errCount)
	writeResult("║  Total elapsed: %v", time.Since(startTime).Round(time.Second))
	if count412 > 0 {
		writeResult("║  ")
		writeResult("║  412 Timeline:")
		for _, r := range allResults {
			if r.Status == "412" {
				writeResult("║    #%d %s: phase=%s, time=%v", r.Index, r.Name, r.Phase, r.Timestamp.Format("15:04:05"))
			}
		}
	} else {
		writeResult("║  No 412 triggered! ✅")
	}
	writeResult("╚══════════════════════════════════════════════════════════════╝")
}

// TestProbeStage1_RecoveryAfter412 first triggers 412 using large-scale crawling,
// then tests different recovery strategies:
//  1. Pause 5s on same browser
//  2. Pause 10s on same browser
//  3. Close browser + create new one (no pause)
//
// Run: go test -v -run TestProbeStage1_RecoveryAfter412 -timeout 1200s ./probe/
func TestProbeStage1_RecoveryAfter412(t *testing.T) {
	pc := newStage1ProbeConfig(t)

	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	testMids := loadTestMids(t, 60)

	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()

	t.Log("╔══════════════════════════════════════════════════════════════╗")
	t.Log("║  Experiment: Recovery After 412                             ║")
	t.Log("║  Goal: Find the best recovery strategy                     ║")
	t.Log("╚══════════════════════════════════════════════════════════════╝")

	// Step 1: Trigger 412 using sequential crawling (simpler to control).
	t.Log("\n=== Step 1: Trigger 412 by processing authors sequentially ===")

	mgr := pc.createBrowser(t, 3)
	crawler := bilibili.NewAuthorCrawler(mgr)

	maxVideos := 100
	var found412 bool

	for i, mid := range testMids {
		if ctx.Err() != nil {
			break
		}
		result := processOneAuthorProbe(ctx, crawler, mid, i+1, maxVideos)
		t.Logf("  Author #%d %s: status=%s, duration=%v", i+1, mid.Name, result.Status, result.Duration.Round(time.Millisecond))

		if result.Status == "412" {
			found412 = true
			t.Logf("  ⚠️ 412 triggered at author #%d!", i+1)
			break
		}
	}

	if !found412 {
		mgr.Close()
		t.Log("❌ Could not trigger 412 with sequential processing.")
		t.Log("Try running TestProbeStage1_Concurrent first to confirm 412 is reproducible.")
		return
	}

	// Step 2: Test recovery — pause 5s on same browser.
	t.Log("\n=== Step 2: Recovery test — pause 5s, same browser ===")
	time.Sleep(5 * time.Second)

	// Use a fresh mid that we haven't tried yet.
	recoveryMid := src.AuthorMid{Name: "recovery_test_5s", ID: "314216"}
	result := processOneAuthorProbe(ctx, crawler, recoveryMid, 0, 10)
	t.Logf("  After 5s pause: status=%s, phase=%s, duration=%v", result.Status, result.Phase, result.Duration.Round(time.Millisecond))
	if result.Status == "ok" {
		t.Log("  ✅ Recovery with 5s pause WORKS!")
		mgr.Close()
		return
	}

	// Step 3: Test recovery — pause 10s on same browser.
	t.Log("\n=== Step 3: Recovery test — pause 10s, same browser ===")
	time.Sleep(10 * time.Second)

	recoveryMid = src.AuthorMid{Name: "recovery_test_10s", ID: "546195"}
	result = processOneAuthorProbe(ctx, crawler, recoveryMid, 0, 10)
	t.Logf("  After 10s pause: status=%s, phase=%s, duration=%v", result.Status, result.Phase, result.Duration.Round(time.Millisecond))
	if result.Status == "ok" {
		t.Log("  ✅ Recovery with 10s pause WORKS!")
		mgr.Close()
		return
	}

	// Step 4: Test recovery — close browser and create new one.
	t.Log("\n=== Step 4: Recovery test — new browser (no pause) ===")
	mgr.Close()

	mgr2 := pc.createBrowser(t, 3)
	defer mgr2.Close()
	crawler2 := bilibili.NewAuthorCrawler(mgr2)

	recoveryMid = src.AuthorMid{Name: "recovery_test_new_browser", ID: "946974"}
	result = processOneAuthorProbe(ctx, crawler2, recoveryMid, 0, 10)
	t.Logf("  After new browser: status=%s, phase=%s, duration=%v", result.Status, result.Phase, result.Duration.Round(time.Millisecond))
	if result.Status == "ok" {
		t.Log("  ✅ Recovery with new browser WORKS!")
	} else {
		t.Log("  ❌ New browser also fails. 412 is likely IP-based.")
	}

	// Summary.
	t.Log("\n╔══════════════════════════════════════════════════════════════╗")
	t.Log("║                  RECOVERY SUMMARY                          ║")
	t.Log("╠══════════════════════════════════════════════════════════════╣")
	t.Log("║  Test results above show which recovery strategy works.    ║")
	t.Log("║  Use this to implement automatic 412 recovery in Stage 1.  ║")
	t.Log("╚══════════════════════════════════════════════════════════════╝")
}
