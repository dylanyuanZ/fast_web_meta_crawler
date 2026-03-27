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
		{URLPattern: "/x/space/acc/info", ID: "user_info"},
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

// FetchAuthorVideos opens the author's video tab and intercepts the video list API.
// URL: https://space.bilibili.com/{mid}/video?pn={page}
// Intercepts: /x/space/wbi/arc/search
func (c *BiliBrowserAuthorCrawler) FetchAuthorVideos(ctx context.Context, mid string, page int) ([]src.VideoDetail, src.PageInfo, error) {
	p := c.manager.GetPage()
	defer c.manager.PutPage(p)

	targetURL := fmt.Sprintf("https://space.bilibili.com/%s/video?pn=%d", mid, page)

	rules := []browser.InterceptRule{{
		URLPattern: "/x/space/wbi/arc/search",
		ID:         "video_list",
	}}

	results, err := browser.NavigateAndIntercept(ctx, p, targetURL, rules)
	if err != nil {
		return nil, src.PageInfo{}, fmt.Errorf("fetch videos mid=%s page=%d: %w", mid, page, err)
	}

	// Extract response body.
	var videoBody []byte
	for _, r := range results {
		if r.ID == "video_list" {
			videoBody = r.Body
			break
		}
	}
	if videoBody == nil {
		return nil, src.PageInfo{}, fmt.Errorf("fetch videos mid=%s page=%d: video list API not intercepted", mid, page)
	}

	// Parse JSON response.
	var resp VideoListResp
	if err := json.Unmarshal(videoBody, &resp); err != nil {
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
	if videoPageSize > 0 {
		totalPages = int(math.Ceil(float64(totalCount) / float64(videoPageSize)))
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
