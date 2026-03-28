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

// SearchPage opens Bilibili search page and extracts search results from Pinia SSR data.
// Bilibili search pages are server-side rendered — the search result data is embedded
// in the HTML via window.__pinia (Vue 3 Pinia state), not fetched via a separate API call.
// The data structure in Pinia is identical to the SearchData type (same as the API response's data field).
//
// URL: https://search.bilibili.com/video?keyword={keyword}&page={page}
func (c *BiliBrowserSearchCrawler) SearchPage(ctx context.Context, keyword string, page int) ([]src.Video, src.PageInfo, error) {
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
	videos := make([]src.Video, 0, len(data.Result))
	for _, item := range data.Result {
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
		TotalPages: data.NumPages,
		TotalCount: data.NumTotal,
	}

	log.Printf("INFO: [bilibili] Search page %d: %d videos found (total pages: %d)", page, len(videos), pageInfo.TotalPages)
	return videos, pageInfo, nil
}
