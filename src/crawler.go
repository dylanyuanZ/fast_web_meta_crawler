package src

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/pool"
)

// PageInfo carries pagination metadata returned by API responses.
type PageInfo struct {
	TotalPages int // total pages available
	TotalCount int // total items available
}

// AuthorMid represents a unique author identifier, passed from stage 0 to stage 1.
type AuthorMid struct {
	Name string `json:"name"` // author display name (for logging/progress)
	ID   string `json:"id"`   // platform-specific user ID (e.g. Bilibili mid)
}

// SearchRecorder defines the platform-specific search + record capability.
// Each platform implements this interface to provide keyword-based video search
// with integrated CSV writing.
type SearchRecorder interface {
	SearchAndRecord(ctx context.Context, keyword string, csvWriter CSVRowWriter, progress ProgressTracker) (int, error)
}

// AuthorCrawler defines the platform-specific author detail capability.
type AuthorCrawler interface {
	// FetchAuthorInfo fetches basic author info and returns it as a CSV row.
	FetchAuthorInfo(ctx context.Context, mid string) ([]string, error)

	// FetchAllAuthorVideos fetches all videos for an author and returns them as CSV rows.
	FetchAllAuthorVideos(ctx context.Context, mid string, maxVideos int) ([][]string, error)
}

// RunStage0 orchestrates stage 0: search → record → deduplicate → export.
// The search+record logic is delegated to the platform's SearchRecorder.
func RunStage0(ctx context.Context, sr SearchRecorder, keyword string, cfg Stage0Config) ([]AuthorMid, error) {
	start := time.Now()
	log.Printf("INFO: Stage 0 started, keyword=%q", keyword)

	// Step 1: Create or open CSVWriter.
	var csvWriter CSVRowWriter
	var err error
	if cfg.ExistingVideoCSVPath != "" {
		csvWriter, err = cfg.OpenCSVWriter(cfg.ExistingVideoCSVPath)
	} else {
		csvWriter, err = cfg.NewCSVWriter(cfg.OutputDir, cfg.Platform, keyword)
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

	// Step 3: Delegate search+record to platform.
	totalVideos, err := sr.SearchAndRecord(ctx, keyword, csvWriter, cfg.Progress)
	if err != nil {
		return nil, fmt.Errorf("search and record: %w", err)
	}

	// Step 4: Close CSV writer before reading.
	if err := csvWriter.Close(); err != nil {
		log.Printf("WARN: failed to close video CSV writer: %v", err)
	}

	// Step 5: Read CSV to extract unique authors.
	mids, err := cfg.ReadVideoCSVAuthors(csvWriter.FilePath())
	if err != nil {
		return nil, fmt.Errorf("read video CSV for dedup: %w", err)
	}

	log.Printf("INFO: [Stage 0] Total videos: %d, Unique authors: %d", totalVideos, len(mids))
	log.Printf("INFO: [Stage 0] Video CSV: %s", csvWriter.FilePath())

	// Step 6: Write intermediate data file (author mids as JSON).
	if err := writeIntermediateData(cfg.OutputDir, cfg.Platform, keyword, mids); err != nil {
		return nil, fmt.Errorf("write intermediate data: %w", err)
	}

	// Step 7: Update progress to stage 1.
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
func RunStage1(ctx context.Context, ac AuthorCrawler, mids []AuthorMid, cfg Stage1Config) error {
	start := time.Now()
	concurrency := config.Get().GetPlatformConcurrency(cfg.Platform)
	requestInterval := config.Get().GetPlatformRequestInterval(cfg.Platform)
	maxConsecutiveFailures := config.Get().MaxConsecutiveFailures
	log.Printf("INFO: Stage 1 started, %d authors to process, concurrency=%d", len(mids), concurrency)

	if len(mids) == 0 {
		log.Printf("INFO: [Stage 1] No authors to process, skipping")
		return nil
	}

	// Step 1: Create or open CSVWriter.
	var csvWriter CSVRowWriter
	var err error
	if cfg.ExistingCSVPath != "" {
		csvWriter, err = cfg.OpenCSVWriter(cfg.ExistingCSVPath)
	} else {
		csvWriter, err = cfg.NewCSVWriter(cfg.OutputDir, cfg.Platform, cfg.Keyword)
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

	// Step 3: Worker Pool — fetch author info and write to CSV.
	results := cfg.PoolRun(ctx, concurrency, mids,
		func(ctx context.Context, mid AuthorMid) ([]string, error) {
			row, err := ac.FetchAuthorInfo(ctx, mid.ID)
			if err != nil {
				return nil, err
			}
			if writeErr := csvWriter.WriteRow(row); writeErr != nil {
				log.Printf("WARN: failed to write author %s to CSV: %v", mid.ID, writeErr)
			}
			log.Printf("INFO: Author %s (mid=%s) processed", mid.Name, mid.ID)
			return row, nil
		},
		maxConsecutiveFailures,
		requestInterval,
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
	concurrency := config.Get().GetPlatformConcurrency(cfg.Platform)
	requestInterval := config.Get().GetPlatformRequestInterval(cfg.Platform)
	maxConsecutiveFailures := config.Get().MaxConsecutiveFailures
	maxVideoPerAuthor := config.Get().MaxVideoPerAuthor
	log.Printf("INFO: Stage 2 started, %d authors to process, concurrency=%d", len(mids), concurrency)

	if len(mids) == 0 {
		log.Printf("INFO: [Stage 2] No authors to process, skipping")
		return nil
	}

	// Step 1: Create or open CSVWriter.
	var csvWriter CSVRowWriter
	var err error
	if cfg.ExistingCSVPath != "" {
		csvWriter, err = cfg.OpenCSVWriter(cfg.ExistingCSVPath)
	} else {
		csvWriter, err = cfg.NewCSVWriter(cfg.OutputDir, cfg.Platform, cfg.Keyword)
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
	results := cfg.PoolRun(ctx, concurrency, mids,
		func(ctx context.Context, mid AuthorMid) ([]string, error) {
			cd.Wait(ctx)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			row, err := retryWithCooldown(ctx, 3, cd,
				fmt.Sprintf("author %s (mid=%s)", mid.Name, mid.ID),
				func() ([]string, error) {
					return processOneAuthorFull(ctx, ac, mid, cfg, maxVideoPerAuthor, requestInterval)
				},
				cfg.IsRetryableError,
			)
			if err != nil {
				return nil, err
			}

			if writeErr := csvWriter.WriteRow(row); writeErr != nil {
				log.Printf("WARN: failed to write author %s to CSV: %v", mid.ID, writeErr)
			}
			return row, nil
		},
		maxConsecutiveFailures,
		requestInterval,
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

// processOneAuthorFull fetches author info + all videos and merges them into a single CSV row.
// The platform's FetchAuthorInfo returns the base row, and FetchAllAuthorVideos returns
// additional video detail rows. The MergeAuthorRow function combines them.
func processOneAuthorFull(ctx context.Context, ac AuthorCrawler, mid AuthorMid, cfg Stage2Config, maxVideoPerAuthor int, requestInterval time.Duration) ([]string, error) {
	authorStart := time.Now()

	// Step 1: Fetch author info (returns CSV row).
	infoRow, err := ac.FetchAuthorInfo(ctx, mid.ID)
	if err != nil {
		return nil, fmt.Errorf("fetch author info: %w", err)
	}

	// Brief pause between API calls with jitter.
	if requestInterval > 0 {
		time.Sleep(pool.JitteredDuration(requestInterval))
	}

	// Step 2: Fetch all videos (returns CSV rows).
	videoRows, err := ac.FetchAllAuthorVideos(ctx, mid.ID, maxVideoPerAuthor)
	if err != nil {
		return nil, fmt.Errorf("fetch author videos: %w", err)
	}

	// Step 3: Merge info + video stats into final row.
	var finalRow []string
	if cfg.MergeAuthorRow != nil {
		finalRow = cfg.MergeAuthorRow(infoRow, videoRows)
	} else {
		finalRow = infoRow
	}

	log.Printf("INFO: Author %s: %d videos fetched, %v",
		mid.Name, len(videoRows), time.Since(authorStart).Round(time.Millisecond))
	return finalRow, nil
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
	Platform             string
	OutputDir            string
	Progress             ProgressTracker
	NewCSVWriter         func(outputDir, platform, keyword string) (CSVRowWriter, error)
	OpenCSVWriter        func(existingPath string) (CSVRowWriter, error)
	ReadVideoCSVAuthors  func(csvPath string) ([]AuthorMid, error)
	ExistingVideoCSVPath string // non-empty when resuming from a previous run
}

// Stage1Config holds dependencies for RunStage1 (basic author info, no video traversal).
type Stage1Config struct {
	Platform        string
	Keyword         string
	OutputDir       string
	Progress        ProgressTracker
	PoolRun         PoolRunFunc[AuthorMid, []string]
	NewCSVWriter    func(outputDir, platform, keyword string) (CSVRowWriter, error)
	OpenCSVWriter   func(existingPath string) (CSVRowWriter, error)
	ExistingCSVPath string // non-empty when resuming from a previous run
}

// Stage2Config holds dependencies for RunStage2 (full author info with video traversal).
type Stage2Config struct {
	Platform         string
	Keyword          string
	OutputDir        string
	Progress         ProgressTracker
	PoolRun          PoolRunFunc[AuthorMid, []string]
	NewCSVWriter     func(outputDir, platform, keyword string) (CSVRowWriter, error)
	OpenCSVWriter    func(existingPath string) (CSVRowWriter, error)
	ExistingCSVPath  string // non-empty when resuming from a previous run
	MergeAuthorRow   func(infoRow []string, videoRows [][]string) []string
	Cooldown         *pool.Cooldown
	IsRetryableError func(err error) bool
}

// CSVRowWriter abstracts incremental CSV writing for generic row data.
type CSVRowWriter interface {
	WriteRow(row []string) error
	WriteRows(rows [][]string) error
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

func intermediateFileName(platform, keyword string) string {
	return fmt.Sprintf("%s_%s_authors.json", platform, keyword)
}

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
