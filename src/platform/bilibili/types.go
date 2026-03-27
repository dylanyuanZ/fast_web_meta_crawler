package bilibili

import "fmt"

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

// ==================== API Error ====================

// retryableAPICodes lists Bilibili API error codes that are transient and should be retried.
// -799: request too frequent; -352: risk control check failed.
var retryableAPICodes = map[int]bool{
	-799: true,
	-352: true,
}

// apiError represents a Bilibili API business error with a code and message.
// It implements the error interface and supports retry detection via IsRetryable().
type apiError struct {
	Code    int
	Message string
	Context string // e.g. "search page 3", "user info mid=123"
}

func (e *apiError) Error() string {
	return fmt.Sprintf("API error (code=%d, message=%s) [%s]", e.Code, e.Message, e.Context)
}

// IsRetryable returns true if this API error code is known to be transient.
func (e *apiError) IsRetryable() bool {
	return retryableAPICodes[e.Code]
}
