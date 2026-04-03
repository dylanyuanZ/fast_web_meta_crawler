package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/pool"
	"github.com/go-rod/rod"
)

// BiliBrowserAuthorCrawler implements src.AuthorCrawler using browser automation.
type BiliBrowserAuthorCrawler struct {
	manager            *browser.Manager
	paginationInterval time.Duration // delay between pagination clicks (defaults to defaultPaginationInterval)
}

// Compile-time interface check.
var _ src.AuthorCrawler = (*BiliBrowserAuthorCrawler)(nil)

// NewAuthorCrawler creates a new BiliBrowserAuthorCrawler.
func NewAuthorCrawler(manager *browser.Manager) *BiliBrowserAuthorCrawler {
	return &BiliBrowserAuthorCrawler{
		manager:            manager,
		paginationInterval: defaultPaginationInterval,
	}
}

// SetPaginationInterval sets the delay between pagination clicks.
func (c *BiliBrowserAuthorCrawler) SetPaginationInterval(d time.Duration) {
	c.paginationInterval = d
}

// FetchAuthorInfo opens the author's space page and intercepts user info, stat,
// upstat, and video list APIs. Returns the author info as a CSV row ([]string).
func (c *BiliBrowserAuthorCrawler) FetchAuthorInfo(ctx context.Context, mid string) ([]string, error) {
	page := c.manager.GetPage()
	defer c.manager.PutPage(page)

	targetURL := fmt.Sprintf("https://space.bilibili.com/%s", mid)

	rules := []browser.InterceptRule{
		{URLPattern: "/x/space/wbi/acc/info", ID: "user_info"},
		{URLPattern: "/x/relation/stat", ID: "user_stat"},
		{URLPattern: "/x/space/upstat", ID: "up_stat"},
		{URLPattern: "/x/space/wbi/arc/search", ID: "video_list"},
	}

	results, err := browser.NavigateAndIntercept(ctx, page, targetURL, rules)
	if err != nil {
		return nil, fmt.Errorf("fetch author info mid=%s: %w", mid, err)
	}

	// Extract response bodies.
	var infoBody, statBody, upStatBody, videoListBody []byte
	for _, r := range results {
		switch r.ID {
		case "user_info":
			infoBody = r.Body
		case "user_stat":
			statBody = r.Body
		case "up_stat":
			upStatBody = r.Body
		case "video_list":
			videoListBody = r.Body
		}
	}

	// All four APIs must be intercepted — no degradation.
	if infoBody == nil {
		return nil, fmt.Errorf("fetch author info mid=%s: user info API not intercepted", mid)
	}
	if statBody == nil {
		return nil, fmt.Errorf("fetch author info mid=%s: user stat API not intercepted", mid)
	}
	if upStatBody == nil {
		return nil, fmt.Errorf("fetch author info mid=%s: up stat API not intercepted", mid)
	}
	if videoListBody == nil {
		return nil, fmt.Errorf("fetch author info mid=%s: video list API not intercepted", mid)
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

	// Parse up stat (total likes + total play count).
	var upStatResp UpStatResp
	if err := json.Unmarshal(upStatBody, &upStatResp); err != nil {
		return nil, fmt.Errorf("parse up stat mid=%s: %w", mid, err)
	}
	if upStatResp.Code != 0 {
		return nil, fmt.Errorf("up stat API error mid=%s (code=%d, message=%s)", mid, upStatResp.Code, upStatResp.Message)
	}

	// Parse video list (only for page.count = total video count).
	var videoListResp VideoListResp
	if err := json.Unmarshal(videoListBody, &videoListResp); err != nil {
		return nil, fmt.Errorf("parse video list mid=%s: %w", mid, err)
	}
	if videoListResp.Code != 0 {
		return nil, fmt.Errorf("video list API error mid=%s (code=%d, message=%s)", mid, videoListResp.Code, videoListResp.Message)
	}

	log.Printf("INFO: [bilibili] Author info fetched: mid=%s, name=%s, followers=%d, likes=%d, plays=%d, videos=%d",
		mid, infoResp.Data.Name, statResp.Data.Follower,
		upStatResp.Data.Likes, upStatResp.Data.Archive.View, videoListResp.Data.Page.Count)

	// Build author info and convert to CSV row.
	info := &src.AuthorInfo{
		Name:           infoResp.Data.Name,
		Followers:      statResp.Data.Follower,
		TotalLikes:     upStatResp.Data.Likes,
		TotalPlayCount: upStatResp.Data.Archive.View,
		VideoCount:     videoListResp.Data.Page.Count,
	}

	return AuthorInfoToBasicRow(info, mid), nil
}

// AuthorInfoToBasicRow converts AuthorInfo to a basic CSV row (Stage 1).
// Matches AuthorBasicHeader() columns.
func AuthorInfoToBasicRow(info *src.AuthorInfo, mid string) []string {
	avgPlay := safeDiv(float64(info.TotalPlayCount), float64(info.VideoCount))
	avgLike := safeDiv(float64(info.TotalLikes), float64(info.VideoCount))
	return []string{
		info.Name,
		mid,
		fmt.Sprintf("%d", info.Followers),
		fmt.Sprintf("%d", info.TotalLikes),
		fmt.Sprintf("%d", info.TotalPlayCount),
		fmt.Sprintf("%d", info.VideoCount),
		fmt.Sprintf("%.1f", avgPlay),
		fmt.Sprintf("%.1f", avgLike),
	}
}

// AuthorBasicHeader returns the CSV header for Stage 1 (basic author info).
func AuthorBasicHeader() []string {
	return []string{
		"博主名字", "ID", "粉丝数", "总获赞数", "总播放数", "视频数量",
		"视频平均播放量", "视频平均点赞量",
	}
}

// AuthorFullHeader returns the CSV header for Stage 2 (full author info with video stats).
func AuthorFullHeader() []string {
	return []string{
		"博主名字", "ID", "粉丝数", "总获赞数", "总播放数", "视频数量",
		"视频平均播放量", "视频平均点赞量",
		"视频平均评论数", "视频平均时长",
		"视频_TOP1", "视频_TOP2", "视频_TOP3",
	}
}

// nextPageButtonSelector is the CSS selector for the "next page" button on Bilibili space video tab.
const nextPageButtonSelector = "button.vui_pagenation--btn-side:last-child"

// defaultPaginationInterval is the delay between clicking "next page" buttons.
const defaultPaginationInterval = 800 * time.Millisecond

// FetchAllAuthorVideos navigates to the author's video tab and fetches all videos
// by paginating from page 1 to the last page (or until maxVideos is reached).
// Returns video detail data as CSV rows ([][]string).
func (c *BiliBrowserAuthorCrawler) FetchAllAuthorVideos(ctx context.Context, mid string, maxVideos int) ([][]string, error) {
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
		return nil, fmt.Errorf("fetch videos mid=%s: navigate failed: %w", mid, err)
	}

	var videoBody []byte
	for _, r := range results {
		if r.ID == "video_list" {
			videoBody = r.Body
			break
		}
	}

	if videoBody == nil {
		return nil, fmt.Errorf("fetch videos mid=%s: video list API not intercepted", mid)
	}

	// Validate page 1 response.
	var quickCheck struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(videoBody, &quickCheck); err != nil {
		return nil, fmt.Errorf("fetch videos mid=%s: page 1 response is not valid JSON (likely risk control HTML): %w", mid, err)
	}
	if quickCheck.Code != 0 {
		return nil, fmt.Errorf("fetch videos mid=%s: page 1 API error (code=%d)", mid, quickCheck.Code)
	}

	// Parse page 1 to get videos and total page count.
	firstVideos, pageInfo, err := c.parseVideoListResponse(videoBody, mid, 1)
	if err != nil {
		return nil, err
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
			break
		}

		if len(allVideos) >= maxVideos {
			allVideos = allVideos[:maxVideos]
			break
		}

		pagInterval := c.paginationInterval
		if pagInterval <= 0 {
			pagInterval = defaultPaginationInterval
		}
		time.Sleep(pool.JitteredDuration(pagInterval))

		waitFn := browser.WaitForIntercept(ctx, p, rules)

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

		nextResults, err := waitFn()
		if err != nil {
			if strings.Contains(err.Error(), "status=412") {
				// Convert all collected videos to rows before returning error.
				rows := videoDetailsToRows(allVideos)
				return rows, fmt.Errorf("fetch videos mid=%s: pagination page %d hit rate limit: %w", mid, currentPage, err)
			}
			log.Printf("WARN: [bilibili] intercept page %d mid=%s failed: %v", currentPage, mid, err)
			break
		}

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

		pageVideos, _, err := c.parseVideoListResponse(nextBody, mid, currentPage)
		if err != nil {
			log.Printf("WARN: [bilibili] parse page %d mid=%s failed: %v", currentPage, mid, err)
			break
		}

		allVideos = append(allVideos, pageVideos...)
	}

	if len(allVideos) > maxVideos {
		allVideos = allVideos[:maxVideos]
	}

	log.Printf("INFO: [bilibili] All videos fetched: mid=%s, pages=%d, total=%d", mid, pageInfo.TotalPages, len(allVideos))
	return videoDetailsToRows(allVideos), nil
}

// videoDetailsToRows converts VideoDetail slice to CSV rows.
func videoDetailsToRows(videos []src.VideoDetail) [][]string {
	rows := make([][]string, 0, len(videos))
	for _, v := range videos {
		rows = append(rows, []string{
			v.Title,
			v.BvID,
			fmt.Sprintf("%d", v.PlayCount),
			fmt.Sprintf("%d", v.CommentCount),
			fmt.Sprintf("%d", v.LikeCount),
			fmt.Sprintf("%d", v.Duration),
			v.PubDate.Format("2006-01-02 15:04:05"),
		})
	}
	return rows
}

// MergeAuthorRow merges a basic author info row with video detail rows
// to produce a full Stage 2 CSV row (with stats and TOP videos).
func MergeAuthorRow(infoRow []string, videoRows [][]string) []string {
	// Parse video details from rows to compute stats.
	var totalPlay, totalComment int64
	var totalDuration int

	type videoForTop struct {
		title     string
		bvid      string
		playCount int64
	}
	var videosForTop []videoForTop

	for _, row := range videoRows {
		if len(row) < 7 {
			continue
		}
		var play, comment int64
		var dur int
		fmt.Sscanf(row[2], "%d", &play)
		fmt.Sscanf(row[3], "%d", &comment)
		fmt.Sscanf(row[5], "%d", &dur)
		totalPlay += play
		totalComment += comment
		totalDuration += dur
		videosForTop = append(videosForTop, videoForTop{title: row[0], bvid: row[1], playCount: play})
	}

	count := float64(len(videoRows))
	var avgComment, avgDuration float64
	if count > 0 {
		avgComment = float64(totalComment) / count
		avgDuration = float64(totalDuration) / count
	}

	// Sort by play count descending and get top 3.
	// Simple selection sort for top 3.
	topN := 3
	if topN > len(videosForTop) {
		topN = len(videosForTop)
	}
	for i := 0; i < topN; i++ {
		maxIdx := i
		for j := i + 1; j < len(videosForTop); j++ {
			if videosForTop[j].playCount > videosForTop[maxIdx].playCount {
				maxIdx = j
			}
		}
		videosForTop[i], videosForTop[maxIdx] = videosForTop[maxIdx], videosForTop[i]
	}

	// Build the full row: base info (8 cols) + avg comment + avg duration + TOP3.
	result := make([]string, 0, 13)
	if len(infoRow) >= 8 {
		result = append(result, infoRow[:8]...)
	} else {
		result = append(result, infoRow...)
		for len(result) < 8 {
			result = append(result, "")
		}
	}
	result = append(result, fmt.Sprintf("%.1f", avgComment))
	result = append(result, fmt.Sprintf("%.1f", avgDuration))

	for i := 0; i < 3; i++ {
		if i < topN {
			v := videosForTop[i]
			title := strings.ReplaceAll(v.title, "\"", "\"\"")
			url := fmt.Sprintf("%s%s", VideoURLPrefix, v.bvid)
			result = append(result, fmt.Sprintf(`=HYPERLINK("%s","%s")`, url, title))
		} else {
			result = append(result, "")
		}
	}

	return result
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

	videos := make([]src.VideoDetail, 0, len(resp.Data.List.Vlist))
	for _, item := range resp.Data.List.Vlist {
		videos = append(videos, src.VideoDetail{
			Title:        item.Title,
			BvID:         item.BvID,
			PlayCount:    item.Play,
			CommentCount: item.Comment,
			LikeCount:    0,
			Duration:     parseDuration(item.Length),
			PubDate:      time.Unix(item.Created, 0),
		})
	}

	totalCount := resp.Data.Page.Count
	totalPages := 1
	actualPS := resp.Data.Page.PS
	if actualPS <= 0 {
		actualPS = videoPageSize
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
