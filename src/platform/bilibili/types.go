package bilibili

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// videoPageSize is the Bilibili default page size for video list API in browser mode.
// The browser's space page uses ps=40 by default (verified via network probe on 2026-03-27).
const videoPageSize = 40

// VideoPageSize returns the page size used for video list requests.
// Exposed for the orchestration layer to calculate max pages.
func VideoPageSize() int {
	return videoPageSize
}

// ==================== Search API Response ====================
// API: GET https://api.bilibili.com/x/web-interface/search/type?search_type=video&keyword=xxx&page=N

// SearchResp is the top-level response from Bilibili search API.
type SearchResp struct {
	Code    int        `json:"code"`
	Message string     `json:"message"`
	Data    SearchData `json:"data"`
}

// SearchData holds the search result data.
type SearchData struct {
	NumPages int          `json:"numPages"` // total pages available
	NumTotal int          `json:"numResults"`
	Result   []SearchItem `json:"result"`
}

// SearchItem represents a single video in search results.
type SearchItem struct {
	Title    string `json:"title"`    // video title (may contain <em> tags)
	Author   string `json:"author"`   // UP master name
	Mid      int64  `json:"mid"`      // UP master ID
	Play     int64  `json:"play"`     // play count
	PubDate  int64  `json:"pubdate"`  // publish timestamp (unix seconds)
	Duration string `json:"duration"` // duration string like "12:34"
}

// ==================== User Info API Response ====================
// API: GET https://api.bilibili.com/x/space/acc/info?mid=xxx

// UserInfoResp is the top-level response from Bilibili user info API.
type UserInfoResp struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Data    UserData `json:"data"`
}

// UserData holds user profile information.
type UserData struct {
	Name string `json:"name"` // display name
	Sign string `json:"sign"` // user signature
}

// ==================== User Stat API Response ====================
// API: GET https://api.bilibili.com/x/relation/stat?vmid=xxx

// UserStatResp is the top-level response from Bilibili user stat API.
type UserStatResp struct {
	Code    int          `json:"code"`
	Message string       `json:"message"`
	Data    UserStatData `json:"data"`
}

// UserStatData holds user follower statistics.
type UserStatData struct {
	Follower int64 `json:"follower"` // follower count
}

// ==================== Up Stat API Response ====================
// API: GET https://api.bilibili.com/x/space/upstat?mid=xxx
// Returns total play count and total likes for an UP master.

// UpStatResp is the top-level response from Bilibili up stat API.
type UpStatResp struct {
	Code    int        `json:"code"`
	Message string     `json:"message"`
	Data    UpStatData `json:"data"`
}

// UpStatData holds the UP master's aggregated statistics.
type UpStatData struct {
	Archive UpStatArchive `json:"archive"` // video archive stats
	Likes   int64         `json:"likes"`   // total likes across all content
}

// UpStatArchive holds the UP master's video archive statistics.
type UpStatArchive struct {
	View int64 `json:"view"` // total play count across all videos
}

// ==================== Video List API Response ====================
// API: GET https://api.bilibili.com/x/space/wbi/arc/search?mid=xxx&pn=N&ps=50

// VideoListResp is the top-level response from Bilibili user video list API.
type VideoListResp struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    VideoListData `json:"data"`
}

// VideoListData holds the video list and pagination info.
type VideoListData struct {
	List VideoListItems `json:"list"`
	Page VideoListPage  `json:"page"`
}

// VideoListItems wraps the video array.
type VideoListItems struct {
	Vlist []VideoListItem `json:"vlist"`
}

// VideoListItem represents a single video in the user's video list.
type VideoListItem struct {
	Title       string `json:"title"`
	BvID        string `json:"bvid"`
	Play        int64  `json:"play"`
	Comment     int64  `json:"comment"`
	Length      string `json:"length"` // duration string like "12:34"
	Created     int64  `json:"created"`
	VideoReview int64  `json:"video_review"` // danmaku count (not used)
}

// VideoListPage holds pagination metadata.
type VideoListPage struct {
	PN    int `json:"pn"`    // current page number
	PS    int `json:"ps"`    // page size
	Count int `json:"count"` // total video count
}

// ==================== Helper Functions ====================

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
