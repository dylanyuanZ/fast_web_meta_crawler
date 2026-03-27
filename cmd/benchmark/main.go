package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/dylanyuanZ/fast_web_meta_crawler/src"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/httpclient"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/platform/bilibili"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/pool"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/stats"
)

// runResult holds the result of a single benchmark run.
type runResult struct {
	RunIndex     int      `json:"run_index"`
	TotalAuthors int      `json:"total_authors"`
	Success      int      `json:"success"`
	Failed       int      `json:"failed"`
	SuccessRate  string   `json:"success_rate"`
	Duration     string   `json:"duration"`
	FailedNames  []string `json:"failed_names,omitempty"`
}

func main() {
	const (
		configPath = "conf/config.yaml"
		keyword    = "游戏"
		platform   = "bilibili"
		numAuthors = 20
	)

	// Parse run index from args.
	runIndex := 1
	if len(os.Args) > 1 {
		fmt.Sscanf(os.Args[1], "%d", &runIndex)
	}

	// Load config (global singleton pattern).
	if err := config.Load(configPath); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	cfg := config.Get()

	// Initialize language detector.
	stats.InitDetector()

	// Load intermediate data (author list from stage 0).
	allMids, err := src.LoadIntermediateData(cfg.OutputDir, platform, keyword)
	if err != nil {
		log.Fatalf("Failed to load intermediate data: %v", err)
	}

	// Take only the first N authors.
	if len(allMids) > numAuthors {
		allMids = allMids[:numAuthors]
	}
	log.Printf("INFO: Single run #%d, %d authors", runIndex, len(allMids))
	log.Printf("INFO: Config: concurrency=%d, request_interval=%v, max_retries=%d",
		cfg.Concurrency, cfg.RequestInterval, cfg.HTTP.MaxRetries)

	result := runOnce(runIndex, allMids, cfg)

	log.Printf("RESULT: Run %d: %d/%d success (%s), duration=%s",
		runIndex, result.Success, result.TotalAuthors, result.SuccessRate, result.Duration)
	if len(result.FailedNames) > 0 {
		for _, f := range result.FailedNames {
			log.Printf("RESULT_FAIL: %s", f)
		}
	}

	// Append result to JSON file.
	appendResult(result)
}

func runOnce(runIndex int, mids []src.AuthorMid, cfg *config.Config) runResult {
	start := time.Now()

	// Create a fresh HTTP client for each run.
	client := httpclient.New()
	authorCrawler := bilibili.NewAuthorCrawler(client)
	ctx := context.Background()

	// Run pool directly to get per-author results.
	poolResults := pool.Run(ctx, cfg.Concurrency, mids,
		func(ctx context.Context, mid src.AuthorMid) (string, error) {
			// Fetch author info (tests user info + user stat APIs).
			info, err := authorCrawler.FetchAuthorInfo(ctx, mid.ID)
			if err != nil {
				return "", fmt.Errorf("fetch info: %w", err)
			}

			// Brief pause with jitter.
			if cfg.RequestInterval > 0 {
				time.Sleep(pool.JitteredDuration(cfg.RequestInterval))
			}

			// Fetch first page of videos (tests wbi-signed API).
			_, _, err = authorCrawler.FetchAuthorVideos(ctx, mid.ID, 1)
			if err != nil {
				return "", fmt.Errorf("fetch videos: %w", err)
			}

			return info.Name, nil
		},
		cfg.MaxConsecutiveFailures,
		cfg.RequestInterval,
	)

	duration := time.Since(start)

	// Count results.
	result := runResult{
		RunIndex:     runIndex,
		TotalAuthors: len(mids),
		Duration:     duration.Round(time.Second).String(),
	}

	for _, pr := range poolResults {
		if pr.Err != nil {
			result.Failed++
			result.FailedNames = append(result.FailedNames,
				fmt.Sprintf("%s (mid=%s): %v", pr.Task.Name, pr.Task.ID, pr.Err))
			log.Printf("FAIL: Author %s (mid=%s): %v", pr.Task.Name, pr.Task.ID, pr.Err)
		} else {
			result.Success++
		}
	}

	rate := float64(result.Success) / float64(result.TotalAuthors) * 100
	result.SuccessRate = fmt.Sprintf("%.1f%%", rate)

	return result
}

// appendResult appends a single run result to the benchmark JSON file.
// When 5 results are accumulated, it also prints a summary table.
func appendResult(result runResult) {
	const path = "data/benchmark_results.json"

	// Load existing results.
	var results []runResult
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &results)
	}

	results = append(results, result)

	// Write back.
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		log.Printf("WARN: failed to marshal results: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("WARN: failed to write results: %v", err)
		return
	}
	log.Printf("INFO: Result appended to %s (%d runs total)", path, len(results))

	// Print summary table if we have enough results.
	if len(results) >= 5 {
		printSummary(results)
	}
}

func printSummary(results []runResult) {
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("BENCHMARK SUMMARY - Stage 1 Anti-Crawl Success Rate")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("%-6s %-8s %-8s %-12s %-10s %s\n", "Run", "Success", "Failed", "Rate", "Duration", "Failed Authors")
	fmt.Println(strings.Repeat("-", 70))

	totalSuccess := 0
	totalAuthors := 0

	for _, r := range results {
		totalAuthors += r.TotalAuthors
		totalSuccess += r.Success
		failedStr := ""
		if len(r.FailedNames) > 0 {
			names := make([]string, 0, len(r.FailedNames))
			for _, n := range r.FailedNames {
				parts := strings.SplitN(n, " (", 2)
				names = append(names, parts[0])
			}
			failedStr = strings.Join(names, ", ")
		}
		fmt.Printf("%-6d %-8d %-8d %-12s %-10s %s\n",
			r.RunIndex, r.Success, r.Failed, r.SuccessRate, r.Duration, failedStr)
	}

	fmt.Println(strings.Repeat("-", 70))
	overallRate := float64(totalSuccess) / float64(totalAuthors) * 100
	fmt.Printf("TOTAL  %-8d %-8d %-12s\n", totalSuccess, totalAuthors-totalSuccess, fmt.Sprintf("%.1f%%", overallRate))
	fmt.Println(strings.Repeat("=", 70))
}
