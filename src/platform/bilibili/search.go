package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/pool"
)

// BiliBrowserSearchCrawler implements src.SearchRecorder using browser automation.
type BiliBrowserSearchCrawler struct {
	manager *browser.Manager
}

// Compile-time interface check.
var _ src.SearchRecorder = (*BiliBrowserSearchCrawler)(nil)

// NewSearchCrawler creates a new BiliBrowserSearchCrawler.
func NewSearchCrawler(manager *browser.Manager) *BiliBrowserSearchCrawler {
	return &BiliBrowserSearchCrawler{manager: manager}
}

// SSRExtractJS is the JS expression to extract search result data from Bilibili's
// SSR search page. Bilibili uses Pinia (Vue 3 state management) to store SSR data
// in window.__pinia. The search results are in __pinia.searchTypeResponse.searchTypeResponse.
const SSRExtractJS = `() => {
	const pinia = window.__pinia;
	if (!pinia) return '';

	// The search type response contains the actual search results.
	const str = pinia.searchTypeResponse && pinia.searchTypeResponse.searchTypeResponse;
	if (str) {
		return JSON.stringify(str);
	}

	// Fallback: return the full pinia state for debugging.
	return JSON.stringify({ _source: '__pinia_debug', _keys: Object.keys(pinia) });
}`

// VideoHeader returns the CSV header for Bilibili video search results.
func VideoHeader() []string {
	return []string{
		"标题", "作者", "AuthorID", "播放次数", "发布时间", "视频时长(s)", "来源",
	}
}

// VideoToRow converts a Video to a CSV row matching VideoHeader columns.
func VideoToRow(video Video) []string {
	return []string{
		video.Title,
		video.Author,
		video.AuthorID,
		fmt.Sprintf("%d", video.PlayCount),
		video.PubDate.Format("2006-01-02 15:04:05"),
		fmt.Sprintf("%d", video.Duration),
		video.Source,
	}
}

// VideoAuthorNameCol is the column index for author name in video CSV.
const VideoAuthorNameCol = 1

// VideoAuthorIDCol is the column index for author ID in video CSV.
const VideoAuthorIDCol = 2

// SearchAndRecord implements src.SearchRecorder for Bilibili.
// It searches for videos by keyword, paginates through results using Worker Pool,
// and writes each page's results to CSV in real-time.
func (c *BiliBrowserSearchCrawler) SearchAndRecord(ctx context.Context, keyword string, csvWriter src.CSVRowWriter, progress src.ProgressTracker) (int, error) {
	cfg := config.Get()

	// Step 1: Fetch first page to get total pages.
	firstVideos, pageInfo, err := c.searchPage(ctx, keyword, 1)
	if err != nil {
		return 0, fmt.Errorf("fetch first page: %w", err)
	}

	// Calculate max pages from MaxSearchVideos.
	// pageSize is determined by the number of videos returned on the first page.
	pageSize := len(firstVideos)
	if pageSize == 0 {
		log.Printf("INFO: [bilibili] First page returned 0 videos, nothing to fetch")
		return 0, nil
	}
	maxPages := (cfg.MaxSearchVideos + pageSize - 1) / pageSize // ceil division
	actualPages := pageInfo.TotalPages
	if actualPages > maxPages {
		actualPages = maxPages
	}
	log.Printf("INFO: Search found %d total pages, will fetch %d pages (max_search_videos=%d, page_size=%d)",
		pageInfo.TotalPages, actualPages, cfg.MaxSearchVideos, pageSize)

	completedPages := make(map[int]bool)
	if progress != nil {
		completedPages = progress.CompletedPages()
	}

	// Write first page videos to CSV immediately (only if not already completed).
	if !completedPages[1] {
		rows := videosToRows(firstVideos)
		if writeErr := csvWriter.WriteRows(rows); writeErr != nil {
			log.Printf("WARN: failed to write first page videos to CSV: %v", writeErr)
		}
		completedPages[1] = true
		if progress != nil {
			if err := progress.AddSearchPage(cfg.OutputDir, 1); err != nil {
				log.Printf("WARN: failed to save progress for page 1: %v", err)
			}
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
	totalVideos := len(firstVideos)
	if len(remainingPages) > 0 {
		poolResults := pool.Run(ctx, cfg.GetPlatformConcurrency("bilibili"), remainingPages,
			func(ctx context.Context, page int) ([]Video, error) {
				videos, _, err := c.searchPage(ctx, keyword, page)
				if err != nil {
					return nil, err
				}
				// Write to CSV immediately (real-time persistence).
				rows := videosToRows(videos)
				if writeErr := csvWriter.WriteRows(rows); writeErr != nil {
					log.Printf("WARN: failed to write page %d videos to CSV: %v", page, writeErr)
				}
				// Record progress for this page.
				if progress != nil {
					if saveErr := progress.AddSearchPage(cfg.OutputDir, page); saveErr != nil {
						log.Printf("WARN: failed to save progress for page %d: %v", page, saveErr)
					}
				}
				return videos, nil
			},
			cfg.MaxConsecutiveFailures,
			cfg.GetPlatformRequestInterval("bilibili"),
		)

		successCount := 1 // first page
		failCount := 0
		for _, r := range poolResults {
			if r.Err != nil {
				failCount++
				log.Printf("ERROR: Failed to fetch page %d: %v", r.Task, r.Err)
			} else {
				successCount++
				totalVideos += len(r.Result)
			}
		}
		log.Printf("INFO: [bilibili] Search pages: success=%d, failed=%d", successCount, failCount)
	}

	return totalVideos, nil
}

// videosToRows converts a slice of Videos to CSV rows.
func videosToRows(videos []Video) [][]string {
	rows := make([][]string, 0, len(videos))
	for _, v := range videos {
		rows = append(rows, VideoToRow(v))
	}
	return rows
}

// searchPage opens Bilibili search page and extracts search results from Pinia SSR data.
// This is the internal implementation used by SearchAndRecord.
func (c *BiliBrowserSearchCrawler) searchPage(ctx context.Context, keyword string, page int) ([]Video, src.PageInfo, error) {
	p := c.manager.GetPage()
	defer c.manager.PutPage(p)

	targetURL := fmt.Sprintf("https://search.bilibili.com/video?keyword=%s&page=%d", url.QueryEscape(keyword), page)

	// Extract SSR data from Pinia state.
	rawJSON, err := browser.NavigateAndExtract(ctx, p, targetURL, SSRExtractJS)
	if err != nil {
		return nil, src.PageInfo{}, fmt.Errorf("search page %d: %w", page, err)
	}

	// Pinia stores the search data directly as SearchData (without the code/message/data wrapper).
	var data SearchData
	if err := json.Unmarshal([]byte(rawJSON), &data); err != nil {
		return nil, src.PageInfo{}, fmt.Errorf("parse SSR search data for page %d: %w", page, err)
	}

	if len(data.Result) == 0 {
		log.Printf("WARN: [bilibili] Search page %d: no results in SSR data", page)
		return nil, src.PageInfo{}, nil
	}

	// Convert to common types.
	videos := make([]Video, 0, len(data.Result))
	for _, item := range data.Result {
		videos = append(videos, Video{
			Title:     stripHTMLTags(item.Title),
			Author:    item.Author,
			AuthorID:  strconv.FormatInt(item.Mid, 10),
			PlayCount: item.Play,
			PubDate:   time.Unix(item.PubDate, 0),
			Duration:  parseDuration(item.Duration),
			Source:    "bilibili",
		})
	}

	pageInfoResult := src.PageInfo{
		TotalPages: data.NumPages,
		TotalCount: data.NumTotal,
	}

	log.Printf("INFO: [bilibili] Search page %d: %d videos found (total pages: %d)", page, len(videos), pageInfoResult.TotalPages)
	return videos, pageInfoResult, nil
}
