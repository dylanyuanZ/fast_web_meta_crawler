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
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/export"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/httpclient"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/platform/bilibili"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/pool"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/progress"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/stats"
)

func main() {
	// Parse command-line arguments.
	platform := flag.String("platform", "", "Target platform (e.g. bilibili)")
	keyword := flag.String("keyword", "", "Search keyword")
	stage := flag.String("stage", "all", "Stage to run: 0, 1, or all")
	configPath := flag.String("config", "conf/config.yaml", "Path to config file")
	flag.Parse()

	if *platform == "" || *keyword == "" {
		fmt.Println("Usage: crawler --platform <platform> --keyword <keyword> [--stage 0|1|all] [--config path]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *platform != "bilibili" {
		log.Fatalf("FATAL: unsupported platform: %s (only 'bilibili' is supported)", *platform)
	}

	if *stage != "0" && *stage != "1" && *stage != "all" {
		log.Fatalf("FATAL: invalid stage: %s (must be 0, 1, or all)", *stage)
	}

	// Load configuration.
	if err := config.Load(*configPath); err != nil {
		log.Fatalf("FATAL: failed to load config: %v", err)
	}
	cfg := config.Get()

	log.Printf("INFO: Configuration loaded: platform=%s, keyword=%q, stage=%s, concurrency=%d, output=%s",
		*platform, *keyword, *stage, cfg.Concurrency, cfg.OutputDir)

	// Initialize language detector.
	stats.InitDetector()

	// Setup context with signal handling for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("WARN: Received signal %v, shutting down gracefully...", sig)
		cancel()
	}()

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
	client := httpclient.New()
	searchCrawler := bilibili.NewSearchCrawler(client)
	authorCrawler := bilibili.NewAuthorCrawler(client)

	taskStart := time.Now()
	var mids []src.AuthorMid

	// Run stages.
	runStage0 := *stage == "0" || *stage == "all"
	runStage1 := *stage == "1" || *stage == "all"

	if runStage0 && (prog.Stage == 0 || *stage == "0") {
		stage0Cfg := src.Stage0Config{
			Platform:               *platform,
			OutputDir:              cfg.OutputDir,
			MaxSearchPage:          cfg.MaxSearchPage,
			Concurrency:            cfg.Concurrency,
			MaxConsecutiveFailures: cfg.MaxConsecutiveFailures,
			RequestInterval:        cfg.RequestInterval,
			Progress:               prog,
			PoolRun:                adaptPoolRun[int, []src.Video],
			WriteVideoCSV:          export.WriteVideoCSV,
		}

		var err error
		mids, err = src.RunStage0(ctx, searchCrawler, *keyword, stage0Cfg)
		if err != nil {
			log.Fatalf("FATAL: Stage 0 failed: %v", err)
		}
	}

	if runStage1 {
		// If stage 1 only, load mids from intermediate data or progress file.
		if !runStage0 || mids == nil {
			if prog.Stage >= 1 && len(prog.AuthorMids) > 0 {
				mids = prog.PendingAuthors()
				log.Printf("INFO: Loaded %d pending authors from progress file", len(mids))
			} else {
				var err error
				mids, err = src.LoadIntermediateData(cfg.OutputDir, *platform, *keyword)
				if err != nil {
					log.Fatalf("FATAL: Cannot load author list for stage 1: %v", err)
				}
				log.Printf("INFO: Loaded %d authors from intermediate data file", len(mids))
			}
		}

		stage1Cfg := src.Stage1Config{
			Platform:               *platform,
			Keyword:                *keyword,
			OutputDir:              cfg.OutputDir,
			Concurrency:            cfg.Concurrency,
			MaxVideoPerAuthor:      cfg.MaxVideoPerAuthor,
			VideoPageSize:          bilibili.VideoPageSize(),
			MaxConsecutiveFailures: cfg.MaxConsecutiveFailures,
			RequestInterval:        cfg.RequestInterval,
			Progress:               prog,
			PoolRun:                adaptPoolRun[src.AuthorMid, src.Author],
			WriteAuthorCSV:         export.WriteAuthorCSV,
			CalcAuthorStats:        stats.CalcAuthorStats,
			DetectLanguage:         stats.DetectLanguage,
		}

		if err := src.RunStage1(ctx, authorCrawler, mids, stage1Cfg); err != nil {
			log.Fatalf("FATAL: Stage 1 failed: %v", err)
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
