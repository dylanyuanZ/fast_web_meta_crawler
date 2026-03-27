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
// Internally calls SearchCrawler.SearchPage via Worker Pool, writes video CSV and
// intermediate data file (deduplicated AuthorMid list).
// Returns the AuthorMid list for stage 1 consumption.
func RunStage0(ctx context.Context, sc SearchCrawler, keyword string, cfg Stage0Config) ([]AuthorMid, error) {
	start := time.Now()
	log.Printf("INFO: Stage 0 started, keyword=%q", keyword)

	// Step 1: Fetch first page to get total pages.
	firstVideos, pageInfo, err := sc.SearchPage(ctx, keyword, 1)
	if err != nil {
		return nil, fmt.Errorf("fetch first page: %w", err)
	}

	actualPages := pageInfo.TotalPages
	if actualPages > cfg.MaxSearchPage {
		actualPages = cfg.MaxSearchPage
	}
	log.Printf("INFO: Search found %d total pages, will fetch %d pages", pageInfo.TotalPages, actualPages)

	// Record first page in progress if available.
	allVideos := make([]Video, 0, len(firstVideos)*actualPages)
	allVideos = append(allVideos, firstVideos...)

	completedPages := make(map[int]bool)
	if cfg.Progress != nil {
		completedPages = cfg.Progress.CompletedPages()
	}
	completedPages[1] = true

	if cfg.Progress != nil {
		if err := cfg.Progress.AddSearchPage(cfg.OutputDir, 1); err != nil {
			log.Printf("WARN: failed to save progress for page 1: %v", err)
		}
	}

	// Step 2: Build remaining page tasks (skip already completed pages).
	var remainingPages []int
	for p := 2; p <= actualPages; p++ {
		if !completedPages[p] {
			remainingPages = append(remainingPages, p)
		}
	}

	// Step 3: Worker Pool for remaining pages.
	if len(remainingPages) > 0 {
		results := cfg.PoolRun(ctx, cfg.Concurrency, remainingPages,
			func(ctx context.Context, page int) ([]Video, error) {
				videos, _, err := sc.SearchPage(ctx, keyword, page)
				if err != nil {
					return nil, err
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

		// Collect results.
		successCount := 0
		failCount := 0
		for _, r := range results {
			if r.Err != nil {
				failCount++
				log.Printf("ERROR: Failed to fetch page %d: %v", r.Task, r.Err)
			} else {
				successCount++
				allVideos = append(allVideos, r.Result...)
			}
		}
		log.Printf("INFO: [Stage 0] Pages: success=%d, failed=%d", successCount+1, failCount)
	}

	// Step 4: Deduplicate by AuthorID.
	seen := make(map[string]bool)
	var mids []AuthorMid
	for _, v := range allVideos {
		if v.AuthorID != "" && !seen[v.AuthorID] {
			seen[v.AuthorID] = true
			mids = append(mids, AuthorMid{Name: v.Author, ID: v.AuthorID})
		}
	}

	log.Printf("INFO: [Stage 0] Total videos: %d, Unique authors: %d", len(allVideos), len(mids))

	// Step 5: Write video CSV.
	videoPath, err := cfg.WriteVideoCSV(cfg.OutputDir, allVideos, cfg.Platform, keyword)
	if err != nil {
		return nil, fmt.Errorf("write video CSV: %w", err)
	}
	log.Printf("INFO: [Stage 0] Video CSV written: %s", videoPath)

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

// RunStage1 orchestrates stage 1: iterate authors → fetch details → calc stats → export CSV.
// Internally uses Worker Pool for author-level concurrency.
func RunStage1(ctx context.Context, ac AuthorCrawler, mids []AuthorMid, cfg Stage1Config) error {
	start := time.Now()
	log.Printf("INFO: Stage 1 started, %d authors to process, concurrency=%d", len(mids), cfg.Concurrency)

	if len(mids) == 0 {
		log.Printf("INFO: [Stage 1] No authors to process, skipping")
		return nil
	}

	results := cfg.PoolRun(ctx, cfg.Concurrency, mids,
		func(ctx context.Context, mid AuthorMid) (Author, error) {
			author, err := processOneAuthor(ctx, ac, mid, cfg)
			if err != nil {
				return Author{}, err
			}
			// Mark this author as done in progress.
			if cfg.Progress != nil {
				if saveErr := cfg.Progress.MarkDone(cfg.OutputDir, mid.ID); saveErr != nil {
					log.Printf("WARN: failed to mark author %s as done: %v", mid.ID, saveErr)
				}
			}
			return author, nil
		},
		cfg.MaxConsecutiveFailures,
		cfg.RequestInterval,
	)

	// Collect results.
	var authors []Author
	successCount := 0
	failCount := 0
	for _, r := range results {
		if r.Err != nil {
			failCount++
			log.Printf("ERROR: Failed to fetch author %s (mid=%s): %v", r.Task.Name, r.Task.ID, r.Err)
		} else {
			successCount++
			authors = append(authors, r.Result)
		}
	}

	log.Printf("INFO: [Stage 1] Authors: success=%d, failed=%d", successCount, failCount)

	// Write author CSV.
	authorPath, err := cfg.WriteAuthorCSV(cfg.OutputDir, authors, cfg.Platform, cfg.Keyword)
	if err != nil {
		return fmt.Errorf("write author CSV: %w", err)
	}
	log.Printf("INFO: [Stage 1] Author CSV written: %s", authorPath)
	log.Printf("INFO: [Stage 1] Completed in %v", time.Since(start).Round(time.Second))

	return nil
}

// processOneAuthor fetches all data for a single author and assembles the Author struct.
func processOneAuthor(ctx context.Context, ac AuthorCrawler, mid AuthorMid, cfg Stage1Config) (Author, error) {
	authorStart := time.Now()

	// Step 1: Fetch author info.
	info, err := ac.FetchAuthorInfo(ctx, mid.ID)
	if err != nil {
		return Author{}, fmt.Errorf("fetch author info: %w", err)
	}

	// Brief pause between API calls with jitter to avoid fixed-rhythm detection.
	if cfg.RequestInterval > 0 {
		time.Sleep(pool.JitteredDuration(cfg.RequestInterval))
	}

	// Step 2: Fetch all videos (internally paginates from page 1 to last page).
	allVideos, pageInfo, err := ac.FetchAllAuthorVideos(ctx, mid.ID, cfg.MaxVideoPerAuthor)
	if err != nil {
		return Author{}, fmt.Errorf("fetch author videos: %w", err)
	}

	// Step 3: Calculate stats.
	stats, topVideos := cfg.CalcAuthorStats(allVideos, 3)

	// Step 4: Detect language from video titles.
	titles := make([]string, len(allVideos))
	for i, v := range allVideos {
		titles[i] = v.Title
	}
	language := cfg.DetectLanguage(titles)

	author := Author{
		Name:       info.Name,
		ID:         mid.ID,
		Followers:  info.Followers,
		Region:     info.Region,
		Language:   language,
		VideoCount: pageInfo.TotalCount,
		Stats:      stats,
		TopVideos:  topVideos,
	}

	log.Printf("INFO: Author %s: %d videos fetched, %v", info.Name, len(allVideos), time.Since(authorStart).Round(time.Millisecond))
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
	WriteVideoCSV          func(outputDir string, videos []Video, platform, keyword string) (string, error)
}

// Stage1Config holds dependencies for RunStage1, enabling testability.
type Stage1Config struct {
	Platform               string
	Keyword                string
	OutputDir              string
	Concurrency            int
	MaxVideoPerAuthor      int
	VideoPageSize          int
	MaxConsecutiveFailures int
	RequestInterval        time.Duration
	Progress               ProgressTracker
	PoolRun                PoolRunFunc[AuthorMid, Author]
	WriteAuthorCSV         func(outputDir string, authors []Author, platform, keyword string) (string, error)
	CalcAuthorStats        func(videos []VideoDetail, topN int) (AuthorStats, []TopVideo)
	DetectLanguage         func(titles []string) string
}

// ProgressTracker abstracts progress operations to avoid circular imports with progress package.
type ProgressTracker interface {
	CompletedPages() map[int]bool
	AddSearchPage(outputDir string, page int) error
	SetAuthorMids(outputDir string, mids []AuthorMid) error
	MarkDone(outputDir string, mid string) error
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
