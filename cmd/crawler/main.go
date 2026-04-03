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
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/platform/youtube"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/pool"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/progress"
)

// supportedPlatforms lists all supported platform names.
var supportedPlatforms = map[string]bool{
	"bilibili": true,
	"youtube":  true,
}

// platformConfig holds platform-specific factory functions and settings.
type platformConfig struct {
	searchRecorder src.SearchRecorder
	authorCrawler  src.AuthorCrawler

	// CSV headers.
	videoHeader       []string
	authorBasicHeader []string
	authorFullHeader  []string

	// CSV column indices for extracting author info from video CSV.
	videoAuthorNameCol int
	videoAuthorIDCol   int

	// Stage 2 merge function (nil if Stage 2 is not supported).
	mergeAuthorRow   func(basicRow []string, videoRows [][]string) []string
	isRetryableError func(err error) bool
}

func main() {
	// Parse command-line arguments.
	platform := flag.String("platform", "", "Target platform (e.g. bilibili, youtube)")
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

	if !supportedPlatforms[*platform] {
		log.Fatalf("FATAL: unsupported platform: %s (supported: bilibili, youtube)", *platform)
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
		*platform, *keyword, *stage, cfg.GetPlatformConcurrency(*platform), cfg.OutputDir)
	log.Printf("INFO: Search settings: max_search_videos=%d, max_video_per_author=%d, request_interval=%v",
		cfg.MaxSearchVideos, cfg.MaxVideoPerAuthor, cfg.GetPlatformRequestInterval(*platform))

	// Print platform-specific configuration.
	switch *platform {
	case "youtube":
		ytCfg := cfg.Platform.YouTube
		log.Printf("INFO: [youtube] Platform config: filter_type=%q, filter_duration=%q, filter_upload=%q, search_page_sort_by=%q, author_page_sort_by=%q",
			ytCfg.FilterType, ytCfg.FilterDuration, ytCfg.FilterUpload, ytCfg.SearchPageSortBy, ytCfg.AuthorPageSortBy)
	case "bilibili":
		hasCookie := cfg.GetBilibiliCookie() != ""
		log.Printf("INFO: [bilibili] Platform config: cookie_configured=%v", hasCookie)
	}

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
		Concurrency: cfg.GetPlatformConcurrency(*platform),
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

	// Platform-specific initialization.
	platCfg := buildPlatformConfig(*platform, mgr, cfg)

	// Ensure login state (platform-specific).
	if err := ensurePlatformLogin(ctx, *platform, mgr, cfg); err != nil {
		log.Fatalf("FATAL: %v", err)
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

	taskStart := time.Now()
	var mids []src.AuthorMid

	// Run stages.
	runStage0 := *stage == "0" || *stage == "all"
	runStage1 := *stage == "1"
	runStage2 := *stage == "2" || *stage == "all"

	if runStage0 && (prog.Stage == 0 || *stage == "0") {
		var existingVideoCSVPath string
		if prog.VideoCSVPath != "" && len(prog.SearchPages) > 0 {
			existingVideoCSVPath = prog.VideoCSVPath
			log.Printf("INFO: Resuming Stage 0 with existing video CSV: %s", existingVideoCSVPath)
		}

		stage0Cfg := src.Stage0Config{
			Platform:  *platform,
			OutputDir: cfg.OutputDir,
			Progress:  prog,
			NewCSVWriter: func(outputDir, plat, kw string) (src.CSVRowWriter, error) {
				return export.NewCSVWriter(outputDir, plat, kw, "videos", platCfg.videoHeader)
			},
			OpenCSVWriter: func(existingPath string) (src.CSVRowWriter, error) {
				return export.OpenCSVWriter(existingPath)
			},
			ReadVideoCSVAuthors: func(csvPath string) ([]src.AuthorMid, error) {
				return export.ReadVideoCSVAuthors(csvPath, platCfg.videoAuthorNameCol, platCfg.videoAuthorIDCol)
			},
			ExistingVideoCSVPath: existingVideoCSVPath,
		}

		var err error
		mids, err = src.RunStage0(ctx, platCfg.searchRecorder, *keyword, stage0Cfg)
		if err != nil {
			log.Fatalf("FATAL: Stage 0 failed: %v", err)
		}
	}

	if runStage1 || runStage2 {
		var existingCSVPath string
		if !runStage0 || mids == nil {
			if prog.Stage >= 1 && len(prog.AuthorMids) > 0 {
				mids = prog.AuthorMids
				log.Printf("INFO: Loaded %d authors from progress file", len(mids))

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
			stage1Cfg := src.Stage1Config{
				Platform:  *platform,
				Keyword:   *keyword,
				OutputDir: cfg.OutputDir,
				Progress:  prog,
				PoolRun:   adaptPoolRun[src.AuthorMid, []string],
				NewCSVWriter: func(outputDir, plat, kw string) (src.CSVRowWriter, error) {
					return export.NewCSVWriter(outputDir, plat, kw, "authors", platCfg.authorBasicHeader)
				},
				OpenCSVWriter: func(existingPath string) (src.CSVRowWriter, error) {
					return export.OpenCSVWriter(existingPath)
				},
				ExistingCSVPath: existingCSVPath,
			}

			if err := src.RunStage1(ctx, platCfg.authorCrawler, mids, stage1Cfg); err != nil {
				log.Fatalf("FATAL: Stage 1 failed: %v", err)
			}
		}

		if runStage2 {
			stage2Cfg := src.Stage2Config{
				Platform:  *platform,
				Keyword:   *keyword,
				OutputDir: cfg.OutputDir,
				Progress:  prog,
				PoolRun:   adaptPoolRun[src.AuthorMid, []string],
				NewCSVWriter: func(outputDir, plat, kw string) (src.CSVRowWriter, error) {
					return export.NewCSVWriter(outputDir, plat, kw, "authors", platCfg.authorFullHeader)
				},
				OpenCSVWriter: func(existingPath string) (src.CSVRowWriter, error) {
					return export.OpenCSVWriter(existingPath)
				},
				ExistingCSVPath:  existingCSVPath,
				MergeAuthorRow:   platCfg.mergeAuthorRow,
				IsRetryableError: platCfg.isRetryableError,
			}

			if err := src.RunStage2(ctx, platCfg.authorCrawler, mids, stage2Cfg); err != nil {
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

// buildPlatformConfig creates platform-specific crawlers and configuration.
func buildPlatformConfig(platform string, mgr *browser.Manager, cfg *config.Config) platformConfig {
	switch platform {
	case "bilibili":
		sc := bilibili.NewSearchCrawler(mgr)
		ac := bilibili.NewAuthorCrawler(mgr)
		ac.SetPaginationInterval(cfg.GetPlatformRequestInterval(platform))
		return platformConfig{
			searchRecorder:     sc,
			authorCrawler:      ac,
			videoHeader:        bilibili.VideoHeader(),
			authorBasicHeader:  bilibili.AuthorBasicHeader(),
			authorFullHeader:   bilibili.AuthorFullHeader(),
			videoAuthorNameCol: bilibili.VideoAuthorNameCol,
			videoAuthorIDCol:   bilibili.VideoAuthorIDCol,
			mergeAuthorRow:     bilibili.MergeAuthorRow,
			isRetryableError:   bilibili.IsRetryableError,
		}
	case "youtube":
		sc := youtube.NewSearchRecorder(mgr)
		ac := youtube.NewAuthorCrawler(mgr)
		return platformConfig{
			searchRecorder:     sc,
			authorCrawler:      ac,
			videoHeader:        youtube.VideoHeader(),
			authorBasicHeader:  youtube.AuthorHeader(),
			authorFullHeader:   youtube.AuthorHeader(), // YouTube has no Stage 2 distinction
			videoAuthorNameCol: youtube.VideoAuthorNameCol,
			videoAuthorIDCol:   youtube.VideoAuthorIDCol,
			mergeAuthorRow:     nil, // YouTube Stage 2 is a no-op
			isRetryableError:   nil,
		}
	default:
		log.Fatalf("FATAL: unsupported platform: %s", platform)
		return platformConfig{} // unreachable
	}
}

// ensurePlatformLogin handles platform-specific login requirements.
func ensurePlatformLogin(ctx context.Context, platform string, mgr *browser.Manager, cfg *config.Config) error {
	loginCtx, loginCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer loginCancel()

	switch platform {
	case "bilibili":
		cookie := cfg.GetBilibiliCookie()
		if err := browser.EnsureLogin(loginCtx, mgr, "https://www.bilibili.com", cookie, bilibili.BilibiliLoginChecker); err != nil {
			return fmt.Errorf("failed to ensure bilibili login: %w", err)
		}
	case "youtube":
		// YouTube does not require login — no cookie needed.
		log.Printf("INFO: [youtube] No login required, skipping login check")
	}
	return nil
}

// adaptPoolRun adapts pool.Run to the PoolRunFunc signature used by crawler.go.
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
