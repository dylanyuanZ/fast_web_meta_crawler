package src

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/dylanyuanZ/fast_web_meta_crawler/src/pool"
)

// PageInfo carries pagination metadata returned by API responses.
// Avoids a separate TotalPages() method — the first SearchPage/FetchAllAuthorVideos
// call naturally returns this info as part of the response.
type PageInfo struct {
	TotalPages int // total pages available
	TotalCount int // total items available
}

// AuthorMid represents a unique author identifier, passed from stage 0 to stage 1.
// Also used as the intermediate data file format.
type AuthorMid struct {
	Name string `json:"name"` // author display name (for logging/progress)
	ID   string `json:"id"`   // platform-specific user ID (e.g. Bilibili mid)
}

// SearchCrawler defines the platform-specific search capability.
// Each platform implements this interface to provide keyword-based video search.
type SearchCrawler interface {
	// SearchPage fetches a single page of search results for the given keyword.
	// Returns the videos found on that page and pagination info.
	// The caller uses PageInfo.TotalPages (from the first call) to decide how many
	// pages to fetch in total (capped by config.max_search_page).
	SearchPage(ctx context.Context, keyword string, page int) ([]Video, PageInfo, error)
}

// AuthorCrawler defines the platform-specific author detail capability.
// Each platform implements this interface to provide author info and video list fetching.
type AuthorCrawler interface {
	// FetchAuthorInfo fetches basic author info (name, followers, region, etc.).
	FetchAuthorInfo(ctx context.Context, mid string) (*AuthorInfo, error)

	// FetchAllAuthorVideos navigates to the author's video page and fetches all videos
	// by paginating from page 1 to the last page (or until maxVideos is reached).
	// Internally handles pagination (e.g. clicking "next page" in SPA) within a single
	// browser tab, avoiding repeated navigation.
	FetchAllAuthorVideos(ctx context.Context, mid string, maxVideos int) ([]VideoDetail, PageInfo, error)
}

// RunStage0 orchestrates stage 0: search → paginate → collect → deduplicate → export CSV.
// Each page's videos are written to CSV immediately after fetching (real-time persistence).
// Internally calls SearchCrawler.SearchPage via Worker Pool.
// Returns the AuthorMid list for stage 1 consumption.
func RunStage0(ctx context.Context, sc SearchCrawler, keyword string, cfg Stage0Config) ([]AuthorMid, error) {
	start := time.Now()
	log.Printf("INFO: Stage 0 started, keyword=%q", keyword)

	// Step 1: Create or open VideoCSVWriter.
	var csvWriter VideoCSVRowWriter
	var err error
	if cfg.ExistingVideoCSVPath != "" {
		// Resume: open existing CSV in append mode (no header).
		csvWriter, err = cfg.OpenVideoCSVWriter(cfg.ExistingVideoCSVPath)
	} else {
		// First run: create new CSV with BOM + header.
		csvWriter, err = cfg.NewVideoCSVWriter(cfg.OutputDir, cfg.Platform, keyword)
	}
	if err != nil {
		return nil, fmt.Errorf("create/open video CSV writer: %w", err)
	}
	defer csvWriter.Close()

	// Step 2: Record CSV path in progress.
	if cfg.Progress != nil {
		if saveErr := cfg.Progress.SetVideoCSVPath(cfg.OutputDir, csvWriter.FilePath()); saveErr != nil {
			log.Printf("WARN: failed to save video CSV path to progress: %v", saveErr)
		}
	}

	// Step 3: Fetch first page to get total pages.
	firstVideos, pageInfo, err := sc.SearchPage(ctx, keyword, 1)
	if err != nil {
		return nil, fmt.Errorf("fetch first page: %w", err)
	}

	actualPages := pageInfo.TotalPages
	if actualPages > cfg.MaxSearchPage {
		actualPages = cfg.MaxSearchPage
	}
	log.Printf("INFO: Search found %d total pages, will fetch %d pages", pageInfo.TotalPages, actualPages)

	completedPages := make(map[int]bool)
	if cfg.Progress != nil {
		completedPages = cfg.Progress.CompletedPages()
	}

	// Write first page videos to CSV immediately (only if not already completed in a previous run).
	if !completedPages[1] {
		if writeErr := csvWriter.WriteRows(firstVideos); writeErr != nil {
			log.Printf("WARN: failed to write first page videos to CSV: %v", writeErr)
		}
	}

	// Record first page progress only if not already completed.
	if !completedPages[1] {
		completedPages[1] = true
		if cfg.Progress != nil {
			if err := cfg.Progress.AddSearchPage(cfg.OutputDir, 1); err != nil {
				log.Printf("WARN: failed to save progress for page 1: %v", err)
			}
		}
	}

	// Step 4: Build remaining page tasks (skip already completed pages).
	var remainingPages []int
	for p := 2; p <= actualPages; p++ {
		if !completedPages[p] {
			remainingPages = append(remainingPages, p)
		}
	}

	// Step 5: Worker Pool for remaining pages — worker writes to CSV immediately.
	successCount := 1 // first page already succeeded
	failCount := 0
	if len(remainingPages) > 0 {
		results := cfg.PoolRun(ctx, cfg.Concurrency, remainingPages,
			func(ctx context.Context, page int) ([]Video, error) {
				videos, _, err := sc.SearchPage(ctx, keyword, page)
				if err != nil {
					return nil, err
				}
				// Write to CSV immediately (real-time persistence).
				if writeErr := csvWriter.WriteRows(videos); writeErr != nil {
					log.Printf("WARN: failed to write page %d videos to CSV: %v", page, writeErr)
				}
				// Record progress for this page.
				if cfg.Progress != nil {
					if saveErr := cfg.Progress.AddSearchPage(cfg.OutputDir, page); saveErr != nil {
						log.Printf("WARN: failed to save progress for page %d: %v", page, saveErr)
					}
				}
				return videos, nil
			},
			cfg.MaxConsecutiveFailures,
			cfg.RequestInterval,
		)

		// Collect results (only for counting, data is already in CSV).
		for _, r := range results {
			if r.Err != nil {
				failCount++
				log.Printf("ERROR: Failed to fetch page %d: %v", r.Task, r.Err)
			} else {
				successCount++
			}
		}
	}
	log.Printf("INFO: [Stage 0] Pages: success=%d, failed=%d", successCount, failCount)

	// Step 6: Close CSV writer before reading.
	if err := csvWriter.Close(); err != nil {
		log.Printf("WARN: failed to close video CSV writer: %v", err)
	}

	// Step 7: Read all videos from CSV for deduplication.
	allVideos, err := cfg.ReadVideoCSV(csvWriter.FilePath())
	if err != nil {
		return nil, fmt.Errorf("read video CSV for dedup: %w", err)
	}

	// Step 8: Deduplicate by AuthorID.
	seen := make(map[string]bool)
	var mids []AuthorMid
	for _, v := range allVideos {
		if v.AuthorID != "" && !seen[v.AuthorID] {
			seen[v.AuthorID] = true
			mids = append(mids, AuthorMid{Name: v.Author, ID: v.AuthorID})
		}
	}

	log.Printf("INFO: [Stage 0] Total videos: %d, Unique authors: %d", len(allVideos), len(mids))
	log.Printf("INFO: [Stage 0] Video CSV: %s", csvWriter.FilePath())

	// Step 9: Write intermediate data file (author mids as JSON).
	if err := writeIntermediateData(cfg.OutputDir, cfg.Platform, keyword, mids); err != nil {
		return nil, fmt.Errorf("write intermediate data: %w", err)
	}

	// Step 10: Update progress to stage 1.
	if cfg.Progress != nil {
		if err := cfg.Progress.SetAuthorMids(cfg.OutputDir, mids); err != nil {
			log.Printf("WARN: failed to update progress to stage 1: %v", err)
		}
	}

	log.Printf("INFO: [Stage 0] Completed in %v", time.Since(start).Round(time.Second))
	return mids, nil
}

// RunStage1 orchestrates stage 1: iterate authors → fetch basic info → export CSV.
// Stage 1 is lightweight: only calls FetchAuthorInfo, no video traversal.
// No cooldown or retry — FetchAuthorInfo is a single page load with low risk control risk.
func RunStage1(ctx context.Context, ac AuthorCrawler, mids []AuthorMid, cfg Stage1Config) error {
	start := time.Now()
	log.Printf("INFO: Stage 1 started, %d authors to process, concurrency=%d", len(mids), cfg.Concurrency)

	if len(mids) == 0 {
		log.Printf("INFO: [Stage 1] No authors to process, skipping")
		return nil
	}

	// Step 1: Create or open CSVWriter.
	var csvWriter AuthorCSVRowWriter
	var err error
	if cfg.ExistingCSVPath != "" {
		csvWriter, err = cfg.OpenAuthorCSVWriter(cfg.ExistingCSVPath)
	} else {
		csvWriter, err = cfg.NewAuthorCSVWriter(cfg.OutputDir, cfg.Platform, cfg.Keyword)
	}
	if err != nil {
		return fmt.Errorf("create/open author CSV writer: %w", err)
	}
	defer csvWriter.Close()

	// Step 2: Record CSV path in progress.
	if cfg.Progress != nil {
		if saveErr := cfg.Progress.SetAuthorCSVPath(cfg.OutputDir, csvWriter.FilePath()); saveErr != nil {
			log.Printf("WARN: failed to save author CSV path to progress: %v", saveErr)
		}
	}

	// Step 3: Worker Pool — no cooldown, no retry for Stage 1.
	results := cfg.PoolRun(ctx, cfg.Concurrency, mids,
		func(ctx context.Context, mid AuthorMid) (Author, error) {
			author, err := processOneAuthorBasic(ctx, ac, mid)
			if err != nil {
				return Author{}, err
			}
			if writeErr := csvWriter.WriteRow(author); writeErr != nil {
				log.Printf("WARN: failed to write author %s to CSV: %v", mid.ID, writeErr)
			}
			return author, nil
		},
		cfg.MaxConsecutiveFailures,
		cfg.RequestInterval,
	)

	// Step 4: Collect and log results.
	successCount := 0
	failCount := 0
	for _, r := range results {
		if r.Err != nil {
			failCount++
			log.Printf("ERROR: Failed to fetch author %s (mid=%s): %v", r.Task.Name, r.Task.ID, r.Err)
		} else {
			successCount++
		}
	}

	log.Printf("INFO: [Stage 1] Authors: success=%d, failed=%d", successCount, failCount)
	log.Printf("INFO: [Stage 1] Author CSV: %s", csvWriter.FilePath())
	log.Printf("INFO: [Stage 1] Completed in %v", time.Since(start).Round(time.Second))

	return nil
}

// RunStage2 orchestrates stage 2: iterate authors → fetch info + videos → calc stats → export CSV.
// Stage 2 includes cooldown and retry for risk control resilience.
func RunStage2(ctx context.Context, ac AuthorCrawler, mids []AuthorMid, cfg Stage2Config) error {
	start := time.Now()
	log.Printf("INFO: Stage 2 started, %d authors to process, concurrency=%d", len(mids), cfg.Concurrency)

	if len(mids) == 0 {
		log.Printf("INFO: [Stage 2] No authors to process, skipping")
		return nil
	}

	// Step 1: Create or open CSVWriter.
	var csvWriter AuthorCSVRowWriter
	var err error
	if cfg.ExistingCSVPath != "" {
		csvWriter, err = cfg.OpenAuthorCSVWriter(cfg.ExistingCSVPath)
	} else {
		csvWriter, err = cfg.NewAuthorCSVWriter(cfg.OutputDir, cfg.Platform, cfg.Keyword)
	}
	if err != nil {
		return fmt.Errorf("create/open author CSV writer: %w", err)
	}
	defer csvWriter.Close()

	// Step 2: Record CSV path in progress.
	if cfg.Progress != nil {
		if saveErr := cfg.Progress.SetAuthorCSVPath(cfg.OutputDir, csvWriter.FilePath()); saveErr != nil {
			log.Printf("WARN: failed to save author CSV path to progress: %v", saveErr)
		}
	}

	// Step 3: Create global cooldown for risk control.
	cd := cfg.Cooldown
	if cd == nil {
		cd = &pool.Cooldown{}
	}

	// Step 4: Worker Pool with cooldown and retry.
	results := cfg.PoolRun(ctx, cfg.Concurrency, mids,
		func(ctx context.Context, mid AuthorMid) (Author, error) {
			cd.Wait(ctx)
			if ctx.Err() != nil {
				return Author{}, ctx.Err()
			}

			author, err := retryWithCooldown(ctx, 3, cd,
				fmt.Sprintf("author %s (mid=%s)", mid.Name, mid.ID),
				func() (Author, error) {
					return processOneAuthorFull(ctx, ac, mid, cfg)
				},
				cfg.IsRetryableError,
			)
			if err != nil {
				return Author{}, err
			}

			if writeErr := csvWriter.WriteRow(author); writeErr != nil {
				log.Printf("WARN: failed to write author %s to CSV: %v", mid.ID, writeErr)
			}
			return author, nil
		},
		cfg.MaxConsecutiveFailures,
		cfg.RequestInterval,
	)

	// Step 5: Collect and log results.
	successCount := 0
	failCount := 0
	for _, r := range results {
		if r.Err != nil {
			failCount++
			log.Printf("ERROR: Failed to fetch author %s (mid=%s): %v", r.Task.Name, r.Task.ID, r.Err)
		} else {
			successCount++
		}
	}

	log.Printf("INFO: [Stage 2] Authors: success=%d, failed=%d", successCount, failCount)
	log.Printf("INFO: [Stage 2] Author CSV: %s", csvWriter.FilePath())
	log.Printf("INFO: [Stage 2] Completed in %v", time.Since(start).Round(time.Second))

	return nil
}

// retryWithCooldown retries a function up to maxRetries times with exponential backoff.
// On retryable errors, triggers global cooldown so all workers pause.
// Non-retryable errors are returned immediately without retry.
// The isRetryable function is platform-specific and injected by the caller.
func retryWithCooldown[T any](
	ctx context.Context,
	maxRetries int,
	cd *pool.Cooldown,
	label string,
	fn func() (T, error),
	isRetryable func(error) bool,
) (T, error) {
	var zero T
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(10<<(attempt-1)) * time.Second
			if cd != nil {
				cd.Trigger(backoff)
			}
			log.Printf("WARN: Retrying %s in %v (attempt %d/%d, last error: %v)",
				label, backoff, attempt, maxRetries, lastErr)
			select {
			case <-ctx.Done():
				return zero, fmt.Errorf("context cancelled during retry backoff: %w", ctx.Err())
			case <-time.After(backoff):
			}
		}

		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err

		if isRetryable == nil || !isRetryable(err) {
			return zero, err
		}
	}

	return zero, fmt.Errorf("all %d retries exhausted for %s: %w", maxRetries, label, lastErr)
}

// processOneAuthorBasic fetches only basic author info (Stage 1 path).
// No video traversal, no stats calculation.
func processOneAuthorBasic(ctx context.Context, ac AuthorCrawler, mid AuthorMid) (Author, error) {
	info, err := ac.FetchAuthorInfo(ctx, mid.ID)
	if err != nil {
		return Author{}, fmt.Errorf("fetch author info: %w", err)
	}

	author := Author{
		Name:           info.Name,
		ID:             mid.ID,
		Followers:      info.Followers,
		VideoCount:     info.VideoCount,
		TotalLikes:     info.TotalLikes,
		TotalPlayCount: info.TotalPlayCount,
	}

	log.Printf("INFO: Author %s (mid=%s): followers=%d, videos=%d",
		info.Name, mid.ID, info.Followers, info.VideoCount)
	return author, nil
}

// processOneAuthorFull fetches author info + all videos + stats (Stage 2 path).
func processOneAuthorFull(ctx context.Context, ac AuthorCrawler, mid AuthorMid, cfg Stage2Config) (Author, error) {
	authorStart := time.Now()

	// Step 1: Fetch author info.
	info, err := ac.FetchAuthorInfo(ctx, mid.ID)
	if err != nil {
		return Author{}, fmt.Errorf("fetch author info: %w", err)
	}

	// Brief pause between API calls with jitter.
	if cfg.RequestInterval > 0 {
		time.Sleep(pool.JitteredDuration(cfg.RequestInterval))
	}

	// Step 2: Fetch all videos.
	allVideos, pageInfo, err := ac.FetchAllAuthorVideos(ctx, mid.ID, cfg.MaxVideoPerAuthor)
	if err != nil {
		return Author{}, fmt.Errorf("fetch author videos: %w", err)
	}

	// Step 3: Calculate stats.
	stats, topVideos := cfg.CalcAuthorStats(allVideos, 3)

	author := Author{
		Name:           info.Name,
		ID:             mid.ID,
		Followers:      info.Followers,
		VideoCount:     pageInfo.TotalCount,
		TotalLikes:     info.TotalLikes,
		TotalPlayCount: info.TotalPlayCount,
		Stats:          stats,
		TopVideos:      topVideos,
	}

	log.Printf("INFO: Author %s: %d videos fetched, %v",
		info.Name, len(allVideos), time.Since(authorStart).Round(time.Millisecond))
	return author, nil
}

// ==================== Configuration structs for dependency injection ====================

// PoolRunFunc is the type signature for pool.Run, allowing dependency injection in tests.
type PoolRunFunc[T any, R any] func(ctx context.Context, concurrency int, tasks []T,
	worker func(ctx context.Context, task T) (R, error), maxConsecutiveFailures int,
	requestInterval time.Duration) []PoolResult[T, R]

// PoolResult mirrors pool.TaskResult to avoid circular imports.
type PoolResult[T any, R any] struct {
	Task   T
	Result R
	Err    error
}

// Stage0Config holds dependencies for RunStage0, enabling testability.
type Stage0Config struct {
	Platform               string
	OutputDir              string
	MaxSearchPage          int
	Concurrency            int
	MaxConsecutiveFailures int
	RequestInterval        time.Duration
	Progress               ProgressTracker
	PoolRun                PoolRunFunc[int, []Video]
	NewVideoCSVWriter      func(outputDir, platform, keyword string) (VideoCSVRowWriter, error)
	OpenVideoCSVWriter     func(existingPath string) (VideoCSVRowWriter, error)
	ReadVideoCSV           func(csvPath string) ([]Video, error)
	ExistingVideoCSVPath   string // non-empty when resuming from a previous run
}

// Stage1Config holds dependencies for RunStage1 (basic author info, no video traversal).
type Stage1Config struct {
	Platform               string
	Keyword                string
	OutputDir              string
	Concurrency            int
	MaxConsecutiveFailures int
	RequestInterval        time.Duration
	Progress               ProgressTracker
	PoolRun                PoolRunFunc[AuthorMid, Author]
	NewAuthorCSVWriter     func(outputDir, platform, keyword string) (AuthorCSVRowWriter, error)
	OpenAuthorCSVWriter    func(existingPath string) (AuthorCSVRowWriter, error)
	ExistingCSVPath        string // non-empty when resuming from a previous run
}

// Stage2Config holds dependencies for RunStage2 (full author info with video traversal).
type Stage2Config struct {
	Platform               string
	Keyword                string
	OutputDir              string
	Concurrency            int
	MaxVideoPerAuthor      int
	MaxConsecutiveFailures int
	RequestInterval        time.Duration
	Progress               ProgressTracker
	PoolRun                PoolRunFunc[AuthorMid, Author]
	NewAuthorCSVWriter     func(outputDir, platform, keyword string) (AuthorCSVRowWriter, error)
	OpenAuthorCSVWriter    func(existingPath string) (AuthorCSVRowWriter, error)
	ExistingCSVPath        string // non-empty when resuming from a previous run
	CalcAuthorStats        func(videos []VideoDetail, topN int) (AuthorStats, []TopVideo)
	Cooldown               *pool.Cooldown       // global cooldown for risk control
	IsRetryableError       func(err error) bool // platform-specific retryable error check
}

// AuthorCSVRowWriter abstracts incremental CSV writing for author data.
// Implemented by export.AuthorCSVWriter; defined here to avoid circular imports.
type AuthorCSVRowWriter interface {
	WriteRow(author Author) error
	FilePath() string
	Close() error
}

// VideoCSVRowWriter abstracts incremental CSV writing for video data.
// Implemented by export.VideoCSVWriter; defined here to avoid circular imports.
type VideoCSVRowWriter interface {
	WriteRows(videos []Video) error
	FilePath() string
	Close() error
}

// ProgressTracker abstracts progress operations to avoid circular imports with progress package.
type ProgressTracker interface {
	CompletedPages() map[int]bool
	AddSearchPage(outputDir string, page int) error
	SetAuthorMids(outputDir string, mids []AuthorMid) error
	SetAuthorCSVPath(outputDir string, csvPath string) error
	SetVideoCSVPath(outputDir string, csvPath string) error
}

// ==================== Intermediate data file ====================

// intermediateFileName returns the filename for the intermediate author mids data.
func intermediateFileName(platform, keyword string) string {
	return fmt.Sprintf("%s_%s_authors.json", platform, keyword)
}

// writeIntermediateData writes the deduplicated author mids to a JSON file.
func writeIntermediateData(outputDir, platform, keyword string, mids []AuthorMid) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	data, err := json.MarshalIndent(mids, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal author mids: %w", err)
	}

	path := filepath.Join(outputDir, intermediateFileName(platform, keyword))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write intermediate file: %w", err)
	}

	log.Printf("INFO: Intermediate data written: %s (%d authors)", path, len(mids))
	return nil
}

// LoadIntermediateData reads the intermediate author mids from a JSON file.
func LoadIntermediateData(outputDir, platform, keyword string) ([]AuthorMid, error) {
	path := filepath.Join(outputDir, intermediateFileName(platform, keyword))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read intermediate file: %w", err)
	}

	var mids []AuthorMid
	if err := json.Unmarshal(data, &mids); err != nil {
		return nil, fmt.Errorf("parse intermediate file: %w", err)
	}

	return mids, nil
}
