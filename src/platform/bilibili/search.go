package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/httpclient"
)

const (
	searchAPI = "https://api.bilibili.com/x/web-interface/search/type"
)

// BiliSearchCrawler implements src.SearchCrawler for Bilibili.
type BiliSearchCrawler struct {
	client *httpclient.Client
}

// Compile-time interface check.
var _ src.SearchCrawler = (*BiliSearchCrawler)(nil)

// NewSearchCrawler creates a new BiliSearchCrawler.
func NewSearchCrawler(client *httpclient.Client) *BiliSearchCrawler {
	return &BiliSearchCrawler{client: client}
}

// SearchPage fetches a single page of Bilibili search results.
// Automatically retries on transient API errors (-799, -352).
func (c *BiliSearchCrawler) SearchPage(ctx context.Context, keyword string, page int) ([]src.Video, src.PageInfo, error) {
	var resp SearchResp
	err := retryOnAPIError(ctx, fmt.Sprintf("search page %d", page), func() error {
		url := fmt.Sprintf("%s?search_type=video&keyword=%s&page=%d", searchAPI, keyword, page)
		body, err := c.client.Get(ctx, url)
		if err != nil {
			return fmt.Errorf("search page %d: %w", page, err)
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("parse search response: %w", err)
		}
		if resp.Code != 0 {
			return &apiError{Code: resp.Code, Message: resp.Message, Context: fmt.Sprintf("search page %d", page)}
		}
		return nil
	})
	if err != nil {
		return nil, src.PageInfo{}, err
	}

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

	return videos, pageInfo, nil
}

// htmlTagRegex matches HTML tags like <em class="keyword">.
var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

// stripHTMLTags removes HTML tags from search result titles.
// Bilibili search API wraps matched keywords in <em> tags.
func stripHTMLTags(s string) string {
	return htmlTagRegex.ReplaceAllString(s, "")
}

// parseDuration converts a duration string like "12:34" or "1:02:03" to seconds.
func parseDuration(s string) int {
	parts := strings.Split(s, ":")
	total := 0

	switch len(parts) {
	case 2: // mm:ss
		m, _ := strconv.Atoi(parts[0])
		sec, _ := strconv.Atoi(parts[1])
		total = m*60 + sec
	case 3: // hh:mm:ss
		h, _ := strconv.Atoi(parts[0])
		m, _ := strconv.Atoi(parts[1])
		sec, _ := strconv.Atoi(parts[2])
		total = h*3600 + m*60 + sec
	default:
		// Try parsing as pure seconds.
		n, _ := strconv.Atoi(s)
		total = n
	}

	return int(math.Abs(float64(total)))
}
