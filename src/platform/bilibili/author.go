package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/pool"
	"github.com/go-rod/rod"
)

// BiliBrowserAuthorCrawler implements src.AuthorCrawler using browser automation.
type BiliBrowserAuthorCrawler struct {
	manager *browser.Manager
}

// Compile-time interface check.
var _ src.AuthorCrawler = (*BiliBrowserAuthorCrawler)(nil)

// NewAuthorCrawler creates a new BiliBrowserAuthorCrawler.
func NewAuthorCrawler(manager *browser.Manager) *BiliBrowserAuthorCrawler {
	return &BiliBrowserAuthorCrawler{manager: manager}
}

// FetchAuthorInfo opens the author's space page and intercepts user info + stat APIs.
// URL: https://space.bilibili.com/{mid}
// Intercepts: /x/space/acc/info + /x/relation/stat
func (c *BiliBrowserAuthorCrawler) FetchAuthorInfo(ctx context.Context, mid string) (*src.AuthorInfo, error) {
	page := c.manager.GetPage()
	defer c.manager.PutPage(page)

	targetURL := fmt.Sprintf("https://space.bilibili.com/%s", mid)

	rules := []browser.InterceptRule{
		{URLPattern: "/x/space/wbi/acc/info", ID: "user_info"},
		{URLPattern: "/x/relation/stat", ID: "user_stat"},
	}

	results, err := browser.NavigateAndIntercept(ctx, page, targetURL, rules)
	if err != nil {
		return nil, fmt.Errorf("fetch author info mid=%s: %w", mid, err)
	}

	// Extract response bodies.
	var infoBody, statBody []byte
	for _, r := range results {
		switch r.ID {
		case "user_info":
			infoBody = r.Body
		case "user_stat":
			statBody = r.Body
		}
	}

	if infoBody == nil {
		return nil, fmt.Errorf("fetch author info mid=%s: user info API not intercepted", mid)
	}
	if statBody == nil {
		return nil, fmt.Errorf("fetch author info mid=%s: user stat API not intercepted", mid)
	}

	// Parse user info.
	var infoResp UserInfoResp
	if err := json.Unmarshal(infoBody, &infoResp); err != nil {
		return nil, fmt.Errorf("parse user info mid=%s: %w", mid, err)
	}
	if infoResp.Code != 0 {
		return nil, fmt.Errorf("user info API error mid=%s (code=%d, message=%s)", mid, infoResp.Code, infoResp.Message)
	}

	// Parse user stat.
	var statResp UserStatResp
	if err := json.Unmarshal(statBody, &statResp); err != nil {
		return nil, fmt.Errorf("parse user stat mid=%s: %w", mid, err)
	}
	if statResp.Code != 0 {
		return nil, fmt.Errorf("user stat API error mid=%s (code=%d, message=%s)", mid, statResp.Code, statResp.Message)
	}

	log.Printf("INFO: [bilibili] Author info fetched: mid=%s, name=%s, followers=%d", mid, infoResp.Data.Name, statResp.Data.Follower)

	return &src.AuthorInfo{
		Name:      infoResp.Data.Name,
		Followers: statResp.Data.Follower,
		Region:    "", // Bilibili user info API does not reliably expose region
	}, nil
}

// nextPageButtonSelector is the CSS selector for the "next page" button on Bilibili space video tab.
// Verified via network probe: the pagination uses Vue UI components.
const nextPageButtonSelector = "button.vui_pagenation--btn-side:last-child"

// paginationInterval is the delay between clicking "next page" buttons.
// Slightly shorter than inter-request interval since these are lightweight UI clicks
// within the same page, but still needed to avoid triggering SPA rate limits.
const paginationInterval = 800 * time.Millisecond

// FetchAllAuthorVideos navigates to the author's video tab and fetches all videos
// by paginating from page 1 to the last page (or until maxVideos is reached).
//
// This approach is necessary because Bilibili's space page is a SPA — URL parameters
// like ?pn=2 do NOT affect the API request's pn value. Pagination must be triggered
// by clicking the UI "next page" button.
func (c *BiliBrowserAuthorCrawler) FetchAllAuthorVideos(ctx context.Context, mid string, maxVideos int) ([]src.VideoDetail, src.PageInfo, error) {
	p := c.manager.GetPage()
	defer c.manager.PutPage(p)

	targetURL := fmt.Sprintf("https://space.bilibili.com/%s/video", mid)

	rules := []browser.InterceptRule{{
		URLPattern: "/x/space/wbi/arc/search",
		ID:         "video_list",
	}}

	// Step 1: Navigate to video tab and intercept page 1 API.
	results, err := browser.NavigateAndIntercept(ctx, p, targetURL, rules)
	if err != nil {
		return nil, src.PageInfo{}, fmt.Errorf("fetch videos mid=%s: navigate failed: %w", mid, err)
	}

	var videoBody []byte
	for _, r := range results {
		if r.ID == "video_list" {
			videoBody = r.Body
			break
		}
	}

	if videoBody == nil {
		return nil, src.PageInfo{}, fmt.Errorf("fetch videos mid=%s: video list API not intercepted", mid)
	}

	// Validate page 1 response before attempting pagination.
	// If the initial API response is invalid (e.g. non-JSON from risk control,
	// or API error code), there's no point clicking pagination buttons.
	var quickCheck struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(videoBody, &quickCheck); err != nil {
		return nil, src.PageInfo{}, fmt.Errorf("fetch videos mid=%s: page 1 response is not valid JSON (likely risk control HTML): %w", mid, err)
	}
	if quickCheck.Code != 0 {
		return nil, src.PageInfo{}, fmt.Errorf("fetch videos mid=%s: page 1 API error (code=%d)", mid, quickCheck.Code)
	}

	// Parse page 1 to get videos and total page count.
	firstVideos, pageInfo, err := c.parseVideoListResponse(videoBody, mid, 1)
	if err != nil {
		return nil, src.PageInfo{}, err
	}

	capacity := pageInfo.TotalCount
	if maxVideos < capacity {
		capacity = maxVideos
	}
	allVideos := make([]src.VideoDetail, 0, capacity)
	allVideos = append(allVideos, firstVideos...)

	// Step 2: Click "next page" to fetch remaining pages within the same tab.
	for currentPage := 2; currentPage <= pageInfo.TotalPages; currentPage++ {
		if ctx.Err() != nil {
			return nil, src.PageInfo{}, ctx.Err()
		}

		// Cap at maxVideos.
		if len(allVideos) >= maxVideos {
			allVideos = allVideos[:maxVideos]
			break
		}

		// Brief pause between page clicks with jitter.
		time.Sleep(pool.JitteredDuration(paginationInterval))

		// Set up intercept BEFORE clicking.
		waitFn := browser.WaitForIntercept(ctx, p, rules)

		// Click the "next page" button.
		_, err := p.Eval(fmt.Sprintf(`() => {
			let btn = document.querySelector('%s');
			if (btn && !btn.disabled) {
				btn.click();
				return 'clicked';
			}
			return 'not found or disabled';
		}`, nextPageButtonSelector))
		if err != nil {
			log.Printf("WARN: [bilibili] click next page failed mid=%s page=%d: %v", mid, currentPage, err)
			break
		}

		// Wait for the new API response.
		nextResults, err := waitFn()
		if err != nil {
			log.Printf("WARN: [bilibili] intercept page %d mid=%s failed: %v", currentPage, mid, err)
			break
		}

		// Extract video body from results.
		var nextBody []byte
		for _, r := range nextResults {
			if r.ID == "video_list" {
				nextBody = r.Body
				break
			}
		}
		if nextBody == nil {
			log.Printf("WARN: [bilibili] page %d mid=%s: video list not in intercept results", currentPage, mid)
			break
		}

		// Parse this page's videos.
		pageVideos, _, err := c.parseVideoListResponse(nextBody, mid, currentPage)
		if err != nil {
			log.Printf("WARN: [bilibili] parse page %d mid=%s failed: %v", currentPage, mid, err)
			break
		}

		allVideos = append(allVideos, pageVideos...)
	}

	// Final cap at maxVideos.
	if len(allVideos) > maxVideos {
		allVideos = allVideos[:maxVideos]
	}

	log.Printf("INFO: [bilibili] All videos fetched: mid=%s, pages=%d, total=%d", mid, pageInfo.TotalPages, len(allVideos))
	return allVideos, pageInfo, nil
}

// parseVideoListResponse parses a video list API response body into common types.
func (c *BiliBrowserAuthorCrawler) parseVideoListResponse(body []byte, mid string, page int) ([]src.VideoDetail, src.PageInfo, error) {
	var resp VideoListResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, src.PageInfo{}, fmt.Errorf("parse video list mid=%s page=%d: %w", mid, page, err)
	}
	if resp.Code != 0 {
		return nil, src.PageInfo{}, fmt.Errorf("video list API error mid=%s page=%d (code=%d, message=%s)", mid, page, resp.Code, resp.Message)
	}

	// Convert to common types.
	videos := make([]src.VideoDetail, 0, len(resp.Data.List.Vlist))
	for _, item := range resp.Data.List.Vlist {
		videos = append(videos, src.VideoDetail{
			Title:        item.Title,
			BvID:         item.BvID,
			PlayCount:    item.Play,
			CommentCount: item.Comment,
			LikeCount:    0, // Bilibili video list API does not return like count directly
			Duration:     parseDuration(item.Length),
			PubDate:      time.Unix(item.Created, 0),
		})
	}

	totalCount := resp.Data.Page.Count
	totalPages := 1
	// Use the actual page size from the API response (ps field) to calculate total pages.
	// The browser may use different ps values (25 or 40) depending on the page context.
	actualPS := resp.Data.Page.PS
	if actualPS <= 0 {
		actualPS = videoPageSize // fallback to default
	}
	if actualPS > 0 {
		totalPages = int(math.Ceil(float64(totalCount) / float64(actualPS)))
	}

	pageInfo := src.PageInfo{
		TotalPages: totalPages,
		TotalCount: totalCount,
	}

	log.Printf("INFO: [bilibili] Videos fetched: mid=%s, page=%d, count=%d", mid, page, len(videos))
	return videos, pageInfo, nil
}

// BilibiliLoginChecker checks if the browser is logged into Bilibili.
// Checks for the presence of "SESSDATA" cookie which indicates a valid login.
func BilibiliLoginChecker(ctx context.Context, page *rod.Page) (bool, error) {
	cookies, err := page.Cookies([]string{"https://www.bilibili.com"})
	if err != nil {
		return false, fmt.Errorf("get cookies: %w", err)
	}
	for _, c := range cookies {
		if c.Name == "SESSDATA" && c.Value != "" {
			return true, nil
		}
	}
	return false, nil
}
