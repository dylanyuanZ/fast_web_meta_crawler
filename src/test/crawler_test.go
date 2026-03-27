package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/export"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/progress"
)

// ==================== Mock helpers ====================

// mockSearchCrawler simulates a search API that returns deterministic data.
type mockSearchCrawler struct {
	totalPages    int
	videosPerPage int
}

func (m *mockSearchCrawler) SearchPage(ctx context.Context, keyword string, page int) ([]src.Video, src.PageInfo, error) {
	var videos []src.Video
	for i := 1; i <= m.videosPerPage; i++ {
		videos = append(videos, src.Video{
			Title:     fmt.Sprintf("video_p%d_%d", page, i),
			Author:    fmt.Sprintf("author_p%d_%d", page, i),
			AuthorID:  fmt.Sprintf("mid_%d", (page-1)*m.videosPerPage+i),
			PlayCount: int64(page*100 + i),
			PubDate:   time.Date(2024, 1, page, i, 0, 0, 0, time.UTC),
			Duration:  page*60 + i,
			Source:    "bilibili",
		})
	}
	return videos, src.PageInfo{TotalPages: m.totalPages, TotalCount: m.totalPages * m.videosPerPage}, nil
}

// mockAuthorCrawler simulates author detail fetching.
type mockAuthorCrawler struct{}

func (m *mockAuthorCrawler) FetchAuthorInfo(ctx context.Context, mid string) (*src.AuthorInfo, error) {
	return &src.AuthorInfo{
		Name:      "author_" + mid,
		Followers: 1000,
		Region:    "China",
	}, nil
}

func (m *mockAuthorCrawler) FetchAllAuthorVideos(ctx context.Context, mid string, maxVideos int) ([]src.VideoDetail, src.PageInfo, error) {
	return []src.VideoDetail{
		{Title: "detail_" + mid + "_1", BvID: "BV" + mid + "1", PlayCount: 500, CommentCount: 10, LikeCount: 50, Duration: 120},
		{Title: "detail_" + mid + "_2", BvID: "BV" + mid + "2", PlayCount: 300, CommentCount: 5, LikeCount: 30, Duration: 90},
	}, src.PageInfo{TotalPages: 1, TotalCount: 2}, nil
}

// fakePoolRun executes tasks sequentially (no concurrency) for deterministic testing.
func fakePoolRun[T any, R any](ctx context.Context, concurrency int, tasks []T,
	worker func(ctx context.Context, task T) (R, error), maxConsecutiveFailures int,
	requestInterval time.Duration) []src.PoolResult[T, R] {
	var results []src.PoolResult[T, R]
	for _, task := range tasks {
		result, err := worker(ctx, task)
		results = append(results, src.PoolResult[T, R]{Task: task, Result: result, Err: err})
	}
	return results
}

// interruptingPoolRun simulates a run that processes only the first N tasks then "crashes".
func interruptingPoolRun[T any, R any](stopAfter int) src.PoolRunFunc[T, R] {
	return func(ctx context.Context, concurrency int, tasks []T,
		worker func(ctx context.Context, task T) (R, error), maxConsecutiveFailures int,
		requestInterval time.Duration) []src.PoolResult[T, R] {
		var results []src.PoolResult[T, R]
		for i, task := range tasks {
			if i >= stopAfter {
				break // simulate crash
			}
			result, err := worker(ctx, task)
			results = append(results, src.PoolResult[T, R]{Task: task, Result: result, Err: err})
		}
		return results
	}
}

// fakeCalcAuthorStats returns deterministic stats.
func fakeCalcAuthorStats(videos []src.VideoDetail, topN int) (src.AuthorStats, []src.TopVideo) {
	stats := src.AuthorStats{
		AvgPlayCount:    100.0,
		AvgDuration:     60.0,
		AvgCommentCount: 5.0,
		AvgLikeCount:    20.0,
	}
	var topVideos []src.TopVideo
	for i, v := range videos {
		if i >= topN {
			break
		}
		topVideos = append(topVideos, src.TopVideo{
			Title:     v.Title,
			URL:       fmt.Sprintf("https://bilibili.com/video/%s", v.BvID),
			PlayCount: v.PlayCount,
		})
	}
	return stats, topVideos
}

// fakeDetectLanguage returns a fixed language.
func fakeDetectLanguage(titles []string) string {
	return "Chinese"
}

// ==================== Stage 0 Tests ====================

// TestStage0_NormalRun verifies that a normal (no interruption) Stage 0 run
// produces exactly one video CSV file with all expected data.
func TestStage0_NormalRun(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	sc := &mockSearchCrawler{totalPages: 3, videosPerPage: 2}
	prog := progress.NewProgress("bilibili", "test")

	cfg := src.Stage0Config{
		Platform:               "bilibili",
		OutputDir:              tmpDir,
		MaxSearchPage:          10,
		Concurrency:            1,
		MaxConsecutiveFailures: 5,
		RequestInterval:        0,
		Progress:               prog,
		PoolRun:                fakePoolRun[int, []src.Video],
		NewVideoCSVWriter: func(outputDir, platform, keyword string) (src.VideoCSVRowWriter, error) {
			return export.NewVideoCSVWriter(outputDir, platform, keyword)
		},
		OpenVideoCSVWriter: func(existingPath string) (src.VideoCSVRowWriter, error) {
			return export.OpenVideoCSVWriter(existingPath)
		},
		ReadVideoCSV: export.ReadVideoCSV,
	}

	mids, err := src.RunStage0(ctx, sc, "test", cfg)
	if err != nil {
		t.Fatalf("RunStage0 failed: %v", err)
	}

	// Verify: exactly one CSV file in output dir.
	csvFiles := findCSVFiles(t, tmpDir, "videos")
	if len(csvFiles) != 1 {
		t.Fatalf("expected 1 video CSV file, got %d: %v", len(csvFiles), csvFiles)
	}

	// Verify: read back all videos from CSV.
	videos, err := export.ReadVideoCSV(csvFiles[0])
	if err != nil {
		t.Fatalf("ReadVideoCSV failed: %v", err)
	}

	// 3 pages * 2 videos = 6 total videos.
	expectedCount := 3 * 2
	if len(videos) != expectedCount {
		t.Fatalf("expected %d videos in CSV, got %d", expectedCount, len(videos))
	}

	// Verify: all unique author mids extracted.
	if len(mids) != expectedCount {
		t.Fatalf("expected %d unique author mids, got %d", expectedCount, len(mids))
	}

	t.Logf("Stage 0 normal run: OK — 1 CSV file, %d videos, %d authors", len(videos), len(mids))
}

// TestStage0_WithInterruption verifies that after an interruption and resume,
// the final CSV file is unique and contains all data without duplicates.
func TestStage0_WithInterruption(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	sc := &mockSearchCrawler{totalPages: 5, videosPerPage: 2}
	prog := progress.NewProgress("bilibili", "test")

	// === Phase 1: First run — only completes pages 1-2 ===
	// Page 1 is fetched inline. Pool handles pages 2..5, we interrupt after 1 task (page 2).
	cfg1 := src.Stage0Config{
		Platform:               "bilibili",
		OutputDir:              tmpDir,
		MaxSearchPage:          10,
		Concurrency:            1,
		MaxConsecutiveFailures: 5,
		RequestInterval:        0,
		Progress:               prog,
		PoolRun:                interruptingPoolRun[int, []src.Video](1),
		NewVideoCSVWriter: func(outputDir, platform, keyword string) (src.VideoCSVRowWriter, error) {
			return export.NewVideoCSVWriter(outputDir, platform, keyword)
		},
		OpenVideoCSVWriter: func(existingPath string) (src.VideoCSVRowWriter, error) {
			return export.OpenVideoCSVWriter(existingPath)
		},
		ReadVideoCSV: export.ReadVideoCSV,
	}

	_, err := src.RunStage0(ctx, sc, "test", cfg1)
	if err != nil {
		t.Fatalf("RunStage0 phase 1 failed: %v", err)
	}

	// Verify progress state after phase 1.
	completedPages := prog.CompletedPages()
	t.Logf("Phase 1 completed pages: %v", completedPages)
	if !completedPages[1] || !completedPages[2] {
		t.Fatalf("expected pages 1,2 completed, got: %v", completedPages)
	}

	// Verify: CSV file exists with partial data (pages 1,2 = 4 videos).
	csvFiles1 := findCSVFiles(t, tmpDir, "videos")
	if len(csvFiles1) != 1 {
		t.Fatalf("expected 1 video CSV after phase 1, got %d", len(csvFiles1))
	}
	videos1, err := export.ReadVideoCSV(csvFiles1[0])
	if err != nil {
		t.Fatalf("ReadVideoCSV phase 1 failed: %v", err)
	}
	if len(videos1) != 4 {
		t.Fatalf("expected 4 videos after phase 1, got %d", len(videos1))
	}

	// === Phase 2: Resume — should append pages 3,4,5 to the SAME CSV file ===
	existingCSVPath := prog.VideoCSVPath
	if existingCSVPath == "" {
		t.Fatal("progress.VideoCSVPath is empty after phase 1")
	}

	cfg2 := src.Stage0Config{
		Platform:               "bilibili",
		OutputDir:              tmpDir,
		MaxSearchPage:          10,
		Concurrency:            1,
		MaxConsecutiveFailures: 5,
		RequestInterval:        0,
		Progress:               prog,
		PoolRun:                fakePoolRun[int, []src.Video],
		NewVideoCSVWriter: func(outputDir, platform, keyword string) (src.VideoCSVRowWriter, error) {
			return export.NewVideoCSVWriter(outputDir, platform, keyword)
		},
		OpenVideoCSVWriter: func(existingPath string) (src.VideoCSVRowWriter, error) {
			return export.OpenVideoCSVWriter(existingPath)
		},
		ReadVideoCSV:         export.ReadVideoCSV,
		ExistingVideoCSVPath: existingCSVPath,
	}

	mids, err := src.RunStage0(ctx, sc, "test", cfg2)
	if err != nil {
		t.Fatalf("RunStage0 phase 2 failed: %v", err)
	}

	// Verify: still exactly ONE CSV file (not two).
	csvFiles2 := findCSVFiles(t, tmpDir, "videos")
	if len(csvFiles2) != 1 {
		t.Fatalf("expected 1 video CSV after resume, got %d: %v", len(csvFiles2), csvFiles2)
	}

	// Verify: the CSV file path is the same as phase 1.
	if csvFiles2[0] != csvFiles1[0] {
		t.Fatalf("CSV file path changed after resume: %s -> %s", csvFiles1[0], csvFiles2[0])
	}

	// Verify: total videos = 5 pages * 2 = 10, no duplicates.
	videos2, err := export.ReadVideoCSV(csvFiles2[0])
	if err != nil {
		t.Fatalf("ReadVideoCSV phase 2 failed: %v", err)
	}
	expectedTotal := 5 * 2
	if len(videos2) != expectedTotal {
		t.Fatalf("expected %d videos after resume, got %d", expectedTotal, len(videos2))
	}

	// Verify: no duplicate videos (check unique titles).
	titleSet := make(map[string]bool)
	for _, v := range videos2 {
		if titleSet[v.Title] {
			t.Fatalf("duplicate video found: %s", v.Title)
		}
		titleSet[v.Title] = true
	}

	// Verify: correct number of unique authors.
	if len(mids) != expectedTotal {
		t.Fatalf("expected %d unique author mids, got %d", expectedTotal, len(mids))
	}

	t.Logf("Stage 0 with interruption: OK — 1 CSV file, %d videos (no duplicates), %d authors", len(videos2), len(mids))
}

// ==================== Stage 1 Tests ====================

// TestStage1_NormalRun verifies that a normal Stage 1 run produces exactly one
// author CSV file with all expected data.
func TestStage1_NormalRun(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	ac := &mockAuthorCrawler{}
	prog := progress.NewProgress("bilibili", "test")

	mids := []src.AuthorMid{
		{Name: "author1", ID: "100"},
		{Name: "author2", ID: "200"},
		{Name: "author3", ID: "300"},
	}

	cfg := src.Stage1Config{
		Platform:               "bilibili",
		Keyword:                "test",
		OutputDir:              tmpDir,
		Concurrency:            1,
		MaxVideoPerAuthor:      100,
		VideoPageSize:          30,
		MaxConsecutiveFailures: 5,
		RequestInterval:        0,
		Progress:               prog,
		PoolRun:                fakePoolRun[src.AuthorMid, src.Author],
		NewAuthorCSVWriter: func(outputDir, platform, keyword string) (src.AuthorCSVRowWriter, error) {
			return export.NewAuthorCSVWriter(outputDir, platform, keyword)
		},
		OpenAuthorCSVWriter: func(existingPath string) (src.AuthorCSVRowWriter, error) {
			return export.OpenAuthorCSVWriter(existingPath)
		},
		CalcAuthorStats: fakeCalcAuthorStats,
		DetectLanguage:  fakeDetectLanguage,
	}

	if err := src.RunStage1(ctx, ac, mids, cfg); err != nil {
		t.Fatalf("RunStage1 failed: %v", err)
	}

	// Verify: exactly one author CSV file.
	csvFiles := findCSVFiles(t, tmpDir, "authors")
	if len(csvFiles) != 1 {
		t.Fatalf("expected 1 author CSV file, got %d: %v", len(csvFiles), csvFiles)
	}

	// Verify: read back completed authors.
	completedIDs, err := export.ReadCompletedAuthors(csvFiles[0])
	if err != nil {
		t.Fatalf("ReadCompletedAuthors failed: %v", err)
	}
	if len(completedIDs) != 3 {
		t.Fatalf("expected 3 completed authors, got %d", len(completedIDs))
	}
	for _, mid := range mids {
		if !completedIDs[mid.ID] {
			t.Fatalf("author %s (ID=%s) not found in CSV", mid.Name, mid.ID)
		}
	}

	t.Logf("Stage 1 normal run: OK — 1 CSV file, %d authors", len(completedIDs))
}

// TestStage1_WithInterruption verifies that after an interruption and resume,
// the final author CSV file is unique and contains all data without duplicates.
func TestStage1_WithInterruption(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	ac := &mockAuthorCrawler{}
	prog := progress.NewProgress("bilibili", "test")

	allMids := []src.AuthorMid{
		{Name: "author1", ID: "100"},
		{Name: "author2", ID: "200"},
		{Name: "author3", ID: "300"},
		{Name: "author4", ID: "400"},
		{Name: "author5", ID: "500"},
	}

	// === Phase 1: First run — only completes 2 out of 5 authors ===
	cfg1 := src.Stage1Config{
		Platform:               "bilibili",
		Keyword:                "test",
		OutputDir:              tmpDir,
		Concurrency:            1,
		MaxVideoPerAuthor:      100,
		VideoPageSize:          30,
		MaxConsecutiveFailures: 5,
		RequestInterval:        0,
		Progress:               prog,
		PoolRun:                interruptingPoolRun[src.AuthorMid, src.Author](2),
		NewAuthorCSVWriter: func(outputDir, platform, keyword string) (src.AuthorCSVRowWriter, error) {
			return export.NewAuthorCSVWriter(outputDir, platform, keyword)
		},
		OpenAuthorCSVWriter: func(existingPath string) (src.AuthorCSVRowWriter, error) {
			return export.OpenAuthorCSVWriter(existingPath)
		},
		CalcAuthorStats: fakeCalcAuthorStats,
		DetectLanguage:  fakeDetectLanguage,
	}

	if err := src.RunStage1(ctx, ac, allMids, cfg1); err != nil {
		t.Fatalf("RunStage1 phase 1 failed: %v", err)
	}

	// Verify: CSV exists with 2 authors.
	csvFiles1 := findCSVFiles(t, tmpDir, "authors")
	if len(csvFiles1) != 1 {
		t.Fatalf("expected 1 author CSV after phase 1, got %d", len(csvFiles1))
	}
	completedIDs1, err := export.ReadCompletedAuthors(csvFiles1[0])
	if err != nil {
		t.Fatalf("ReadCompletedAuthors phase 1 failed: %v", err)
	}
	if len(completedIDs1) != 2 {
		t.Fatalf("expected 2 completed authors after phase 1, got %d", len(completedIDs1))
	}
	t.Logf("Phase 1 completed authors: %v", completedIDs1)

	// === Phase 2: Resume — simulate main.go's resume logic ===
	existingCSVPath := prog.AuthorCSVPath
	if existingCSVPath == "" {
		t.Fatal("progress.AuthorCSVPath is empty after phase 1")
	}

	// Filter out already completed authors (this is what main.go does).
	var pendingMids []src.AuthorMid
	for _, mid := range allMids {
		if !completedIDs1[mid.ID] {
			pendingMids = append(pendingMids, mid)
		}
	}
	if len(pendingMids) != 3 {
		t.Fatalf("expected 3 pending authors, got %d", len(pendingMids))
	}

	cfg2 := src.Stage1Config{
		Platform:               "bilibili",
		Keyword:                "test",
		OutputDir:              tmpDir,
		Concurrency:            1,
		MaxVideoPerAuthor:      100,
		VideoPageSize:          30,
		MaxConsecutiveFailures: 5,
		RequestInterval:        0,
		Progress:               prog,
		PoolRun:                fakePoolRun[src.AuthorMid, src.Author],
		NewAuthorCSVWriter: func(outputDir, platform, keyword string) (src.AuthorCSVRowWriter, error) {
			return export.NewAuthorCSVWriter(outputDir, platform, keyword)
		},
		OpenAuthorCSVWriter: func(existingPath string) (src.AuthorCSVRowWriter, error) {
			return export.OpenAuthorCSVWriter(existingPath)
		},
		ExistingCSVPath: existingCSVPath,
		CalcAuthorStats: fakeCalcAuthorStats,
		DetectLanguage:  fakeDetectLanguage,
	}

	if err := src.RunStage1(ctx, ac, pendingMids, cfg2); err != nil {
		t.Fatalf("RunStage1 phase 2 failed: %v", err)
	}

	// Verify: still exactly ONE CSV file (not two).
	csvFiles2 := findCSVFiles(t, tmpDir, "authors")
	if len(csvFiles2) != 1 {
		t.Fatalf("expected 1 author CSV after resume, got %d: %v", len(csvFiles2), csvFiles2)
	}

	// Verify: the CSV file path is the same as phase 1.
	if csvFiles2[0] != csvFiles1[0] {
		t.Fatalf("CSV file path changed after resume: %s -> %s", csvFiles1[0], csvFiles2[0])
	}

	// Verify: total 5 authors, no duplicates.
	completedIDs2, err := export.ReadCompletedAuthors(csvFiles2[0])
	if err != nil {
		t.Fatalf("ReadCompletedAuthors phase 2 failed: %v", err)
	}
	if len(completedIDs2) != 5 {
		t.Fatalf("expected 5 completed authors after resume, got %d", len(completedIDs2))
	}

	// Verify: all 5 authors present.
	for _, mid := range allMids {
		if !completedIDs2[mid.ID] {
			t.Fatalf("author %s (ID=%s) missing from final CSV", mid.Name, mid.ID)
		}
	}

	t.Logf("Stage 1 with interruption: OK — 1 CSV file, %d authors (no duplicates)", len(completedIDs2))
}

// ==================== Helper functions ====================

// findCSVFiles returns all CSV files in dir whose name contains the given suffix pattern.
func findCSVFiles(t *testing.T, dir, typePattern string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var csvFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".csv") && strings.Contains(e.Name(), typePattern) {
			csvFiles = append(csvFiles, filepath.Join(dir, e.Name()))
		}
	}
	return csvFiles
}

// Ensure mock types satisfy interfaces at compile time.
var _ src.SearchCrawler = (*mockSearchCrawler)(nil)
var _ src.AuthorCrawler = (*mockAuthorCrawler)(nil)
