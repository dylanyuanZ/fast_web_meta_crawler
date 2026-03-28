package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/applog"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/export"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/platform/bilibili"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/pool"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/progress"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/stats"
)

func main() {
	// Parse command-line arguments.
	platform := flag.String("platform", "", "Target platform (e.g. bilibili)")
	keyword := flag.String("keyword", "", "Search keyword")
	stage := flag.String("stage", "all", "Stage to run: 0, 1, 2, or all")
	configPath := flag.String("config", "conf/config.yaml", "Path to config file")
	limit := flag.Int("limit", 0, "Limit number of authors to process in stage 1 (0 = no limit, for debugging)")
	flag.Parse()

	if *platform == "" || *keyword == "" {
		fmt.Println("Usage: crawler --platform <platform> --keyword <keyword> [--stage 0|1|2|all] [--config path]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Initialize global log: write all log output to both console and log/ directory.
	if err := applog.Init("log"); err != nil {
		log.Printf("WARN: failed to init global log: %v (logs will only go to console)", err)
	}
	defer applog.Close()

	if *platform != "bilibili" {
		log.Fatalf("FATAL: unsupported platform: %s (only 'bilibili' is supported)", *platform)
	}

	if *stage != "0" && *stage != "1" && *stage != "2" && *stage != "all" {
		log.Fatalf("FATAL: invalid stage: %s (must be 0, 1, 2, or all)", *stage)
	}

	// Load configuration.
	if err := config.Load(*configPath); err != nil {
		log.Fatalf("FATAL: failed to load config: %v", err)
	}
	cfg := config.Get()

	log.Printf("INFO: Configuration loaded: platform=%s, keyword=%q, stage=%s, concurrency=%d, output=%s",
		*platform, *keyword, *stage, cfg.Concurrency, cfg.OutputDir)

	// Setup context with signal handling for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Initialize debug log file for browser network diagnostics.
	if err := browser.InitDebugLog("log"); err != nil {
		log.Printf("WARN: failed to init debug log: %v (debug logs will be dropped)", err)
	}
	defer browser.CloseDebugLog()

	// Initialize browser Manager.
	mgr, err := browser.New(browser.Config{
		Headless:    cfg.Browser.IsHeadless(),
		UserDataDir: cfg.Browser.UserDataDir,
		Concurrency: cfg.Concurrency,
		BrowserBin:  cfg.Browser.Bin,
	})
	if err != nil {
		log.Fatalf("FATAL: failed to create browser manager: %v", err)
	}
	defer mgr.Close()

	// Handle signals — close browser on shutdown.
	go func() {
		sig := <-sigCh
		log.Printf("WARN: Received signal %v, shutting down gracefully...", sig)
		mgr.Close()
		cancel()
	}()

	// Ensure login state.
	loginCtx, loginCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer loginCancel()

	if err := browser.EnsureLogin(loginCtx, mgr, "https://www.bilibili.com", cfg.Cookie, bilibili.BilibiliLoginChecker); err != nil {
		log.Fatalf("FATAL: failed to ensure login: %v", err)
	}

	// Check for existing progress file.
	prog := progress.Load(cfg.OutputDir, *platform, *keyword)
	if prog != nil {
		choice := promptUser("Found existing progress file. Continue from last checkpoint? (y/n): ")
		if strings.ToLower(choice) == "y" || strings.ToLower(choice) == "yes" {
			log.Printf("INFO: Resuming from progress file (stage=%d)", prog.Stage)
		} else {
			log.Printf("INFO: Starting fresh, removing old progress file")
			if err := progress.Clean(cfg.OutputDir, *platform, *keyword); err != nil {
				log.Printf("WARN: failed to clean progress file: %v", err)
			}
			prog = nil
		}
	}

	if prog == nil {
		prog = progress.NewProgress(*platform, *keyword)
	}

	// Create platform-specific crawlers.
	searchCrawler := bilibili.NewSearchCrawler(mgr)
	authorCrawler := bilibili.NewAuthorCrawler(mgr)
	// Set pagination interval to match requestInterval so pagination clicks
	// are rate-limited the same as inter-author requests. This prevents
	// rapid pagination from triggering 412 rate limiting.
	authorCrawler.SetPaginationInterval(cfg.RequestInterval)

	// Create platform-specific CSV adapters.
	authorAdapter := &bilibili.BilibiliAuthorCSVAdapter{}
	videoAdapter := &bilibili.BilibiliVideoCSVAdapter{}

	taskStart := time.Now()
	var mids []src.AuthorMid

	// Run stages.
	runStage0 := *stage == "0" || *stage == "all"
	runStage1 := *stage == "1"
	runStage2 := *stage == "2" || *stage == "all"

	if runStage0 && (prog.Stage == 0 || *stage == "0") {
		// Determine if resuming with existing video CSV.
		var existingVideoCSVPath string
		if prog.VideoCSVPath != "" && len(prog.SearchPages) > 0 {
			existingVideoCSVPath = prog.VideoCSVPath
			log.Printf("INFO: Resuming Stage 0 with existing video CSV: %s", existingVideoCSVPath)
		}

		stage0Cfg := src.Stage0Config{
			Platform:               *platform,
			OutputDir:              cfg.OutputDir,
			MaxSearchPage:          cfg.MaxSearchPage,
			Concurrency:            cfg.Concurrency,
			MaxConsecutiveFailures: cfg.MaxConsecutiveFailures,
			RequestInterval:        cfg.RequestInterval,
			Progress:               prog,
			PoolRun:                adaptPoolRun[int, []src.Video],
			NewVideoCSVWriter: func(outputDir, platform, keyword string) (src.VideoCSVRowWriter, error) {
				return export.NewVideoCSVWriter(outputDir, platform, keyword,
					videoAdapter.Header(), videoAdapter.Row)
			},
			OpenVideoCSVWriter: func(existingPath string) (src.VideoCSVRowWriter, error) {
				return export.OpenVideoCSVWriter(existingPath, videoAdapter.Row)
			},
			ReadVideoCSV:         export.ReadVideoCSV,
			ExistingVideoCSVPath: existingVideoCSVPath,
		}

		var err error
		mids, err = src.RunStage0(ctx, searchCrawler, *keyword, stage0Cfg)
		if err != nil {
			log.Fatalf("FATAL: Stage 0 failed: %v", err)
		}
	}

	if runStage1 || runStage2 {
		// If not coming from stage 0, load mids from intermediate data or progress file.
		var existingCSVPath string
		if !runStage0 || mids == nil {
			if prog.Stage >= 1 && len(prog.AuthorMids) > 0 {
				mids = prog.AuthorMids
				log.Printf("INFO: Loaded %d authors from progress file", len(mids))

				// Resume: filter out already completed authors using CSV.
				if prog.AuthorCSVPath != "" {
					existingCSVPath = prog.AuthorCSVPath
					completedIDs, err := export.ReadCompletedAuthors(prog.AuthorCSVPath)
					if err != nil {
						log.Fatalf("FATAL: Failed to read completed authors from CSV: %v", err)
					}
					if len(completedIDs) > 0 {
						var pending []src.AuthorMid
						for _, mid := range mids {
							if !completedIDs[mid.ID] {
								pending = append(pending, mid)
							}
						}
						log.Printf("INFO: Resuming: %d completed, %d pending", len(completedIDs), len(pending))
						mids = pending
					}
				}
			} else {
				var err error
				mids, err = src.LoadIntermediateData(cfg.OutputDir, *platform, *keyword)
				if err != nil {
					log.Fatalf("FATAL: Cannot load author list for stage 1/2: %v", err)
				}
				log.Printf("INFO: Loaded %d authors from intermediate data file", len(mids))
			}
		}

		// Apply --limit for debugging.
		if *limit > 0 && len(mids) > *limit {
			log.Printf("INFO: Limiting to first %d authors (--limit flag)", *limit)
			mids = mids[:*limit]
		}

		if runStage1 && !runStage2 {
			// Stage 1 only: basic author info, no video traversal.
			stage1Cfg := src.Stage1Config{
				Platform:               *platform,
				Keyword:                *keyword,
				OutputDir:              cfg.OutputDir,
				Concurrency:            cfg.Concurrency,
				MaxConsecutiveFailures: cfg.MaxConsecutiveFailures,
				RequestInterval:        cfg.RequestInterval,
				Progress:               prog,
				PoolRun:                adaptPoolRun[src.AuthorMid, src.Author],
				NewAuthorCSVWriter: func(outputDir, platform, keyword string) (src.AuthorCSVRowWriter, error) {
					return export.NewAuthorCSVWriter(outputDir, platform, keyword,
						authorAdapter.BasicHeader(), authorAdapter.BasicRow)
				},
				OpenAuthorCSVWriter: func(existingPath string) (src.AuthorCSVRowWriter, error) {
					return export.OpenAuthorCSVWriter(existingPath, authorAdapter.BasicRow)
				},
				ExistingCSVPath: existingCSVPath,
			}

			if err := src.RunStage1(ctx, authorCrawler, mids, stage1Cfg); err != nil {
				log.Fatalf("FATAL: Stage 1 failed: %v", err)
			}
		}

		if runStage2 {
			// Stage 2: full author info with video traversal.
			stage2Cfg := src.Stage2Config{
				Platform:               *platform,
				Keyword:                *keyword,
				OutputDir:              cfg.OutputDir,
				Concurrency:            cfg.Concurrency,
				MaxVideoPerAuthor:      cfg.MaxVideoPerAuthor,
				MaxConsecutiveFailures: cfg.MaxConsecutiveFailures,
				RequestInterval:        cfg.RequestInterval,
				Progress:               prog,
				PoolRun:                adaptPoolRun[src.AuthorMid, src.Author],
				NewAuthorCSVWriter: func(outputDir, platform, keyword string) (src.AuthorCSVRowWriter, error) {
					return export.NewAuthorCSVWriter(outputDir, platform, keyword,
						authorAdapter.FullHeader(), authorAdapter.FullRow)
				},
				OpenAuthorCSVWriter: func(existingPath string) (src.AuthorCSVRowWriter, error) {
					return export.OpenAuthorCSVWriter(existingPath, authorAdapter.FullRow)
				},
				ExistingCSVPath: existingCSVPath,
				CalcAuthorStats: func(videos []src.VideoDetail, topN int) (src.AuthorStats, []src.TopVideo) {
					return stats.CalcAuthorStats(videos, topN, bilibili.VideoURLPrefix)
				},
			}

			if err := src.RunStage2(ctx, authorCrawler, mids, stage2Cfg); err != nil {
				log.Fatalf("FATAL: Stage 2 failed: %v", err)
			}
		}
	}

	// Clean up progress file on successful completion.
	if err := progress.Clean(cfg.OutputDir, *platform, *keyword); err != nil {
		log.Printf("WARN: failed to clean progress file: %v", err)
	}

	log.Printf("INFO: ========== Task completed in %v ==========", time.Since(taskStart).Round(time.Second))
}

// adaptPoolRun adapts pool.Run to the PoolRunFunc signature used by crawler.go.
// This bridges the pool package types with the src package types to avoid circular imports.
func adaptPoolRun[T any, R any](
	ctx context.Context,
	concurrency int,
	tasks []T,
	worker func(ctx context.Context, task T) (R, error),
	maxConsecutiveFailures int,
	requestInterval time.Duration,
) []src.PoolResult[T, R] {
	poolResults := pool.Run(ctx, concurrency, tasks, worker, maxConsecutiveFailures, requestInterval)

	results := make([]src.PoolResult[T, R], len(poolResults))
	for i, r := range poolResults {
		results[i] = src.PoolResult[T, R]{
			Task:   r.Task,
			Result: r.Result,
			Err:    r.Err,
		}
	}
	return results
}

// promptUser displays a prompt and reads user input from stdin.
func promptUser(prompt string) string {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}
