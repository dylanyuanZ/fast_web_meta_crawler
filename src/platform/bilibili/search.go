package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
)

// BiliBrowserSearchCrawler implements src.SearchCrawler using browser automation.
type BiliBrowserSearchCrawler struct {
	manager *browser.Manager
}

// Compile-time interface check.
var _ src.SearchCrawler = (*BiliBrowserSearchCrawler)(nil)

// NewSearchCrawler creates a new BiliBrowserSearchCrawler.
func NewSearchCrawler(manager *browser.Manager) *BiliBrowserSearchCrawler {
	return &BiliBrowserSearchCrawler{manager: manager}
}

// SearchPage opens Bilibili search page and intercepts the search API response.
// URL: https://search.bilibili.com/video?keyword={keyword}&page={page}
// Intercepts: api.bilibili.com/x/web-interface/search/type
func (c *BiliBrowserSearchCrawler) SearchPage(ctx context.Context, keyword string, page int) ([]src.Video, src.PageInfo, error) {
	p := c.manager.GetPage()
	defer c.manager.PutPage(p)

	targetURL := fmt.Sprintf("https://search.bilibili.com/video?keyword=%s&page=%d", keyword, page)

	rules := []browser.InterceptRule{{
		URLPattern: "/x/web-interface/search/type",
		ID:         "search",
	}}

	results, err := browser.NavigateAndIntercept(ctx, p, targetURL, rules)
	if err != nil {
		return nil, src.PageInfo{}, fmt.Errorf("search page %d: %w", page, err)
	}

	// Find the search result.
	var searchBody []byte
	for _, r := range results {
		if r.ID == "search" {
			searchBody = r.Body
			break
		}
	}
	if searchBody == nil {
		return nil, src.PageInfo{}, fmt.Errorf("search page %d: no search API response intercepted", page)
	}

	// Parse JSON response.
	var resp SearchResp
	if err := json.Unmarshal(searchBody, &resp); err != nil {
		return nil, src.PageInfo{}, fmt.Errorf("parse search response: %w", err)
	}

	if resp.Code != 0 {
		return nil, src.PageInfo{}, fmt.Errorf("search API error (code=%d, message=%s)", resp.Code, resp.Message)
	}

	// Convert to common types.
	videos := make([]src.Video, 0, len(resp.Data.Result))
	for _, item := range resp.Data.Result {
		videos = append(videos, src.Video{
			Title:     stripHTMLTags(item.Title),
			Author:    item.Author,
			AuthorID:  strconv.FormatInt(item.Mid, 10),
			PlayCount: item.Play,
			PubDate:   time.Unix(item.PubDate, 0),
			Duration:  parseDuration(item.Duration),
			Source:    "bilibili",
		})
	}

	pageInfo := src.PageInfo{
		TotalPages: resp.Data.NumPages,
		TotalCount: resp.Data.NumTotal,
	}

	log.Printf("INFO: [bilibili] Search page %d: %d videos found", page, len(videos))
	return videos, pageInfo, nil
}
