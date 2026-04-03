package src

import "time"

// ==================== Stage 0: Search Results ====================

// Video represents a single video from search results (stage 0 output).
// Fields are limited to what the search API returns.
type Video struct {
	Title     string    // video title
	Author    string    // author display name
	AuthorID  string    // platform-specific author ID (e.g. Bilibili mid)
	PlayCount int64     // view/play count
	PubDate   time.Time // publish date
	Duration  int       // duration in seconds
	Source    string    // platform name, e.g. "bilibili"
}

// ==================== Stage 1: Author Details ====================

// AuthorInfo holds basic author profile data returned by the author info API.
// This is raw API data before any stats calculation.
type AuthorInfo struct {
	Name           string // author display name
	Followers      int64  // follower count
	TotalLikes     int64  // total likes across all content (from upstat API)
	TotalPlayCount int64  // total play count across all videos (from upstat API)
	VideoCount     int    // total video count (from arc/search API page.count)
}

// VideoDetail holds detailed video data returned by the author's video list API.
// Contains more fields than Video (comments, likes) needed for stats calculation.
type VideoDetail struct {
	Title        string    // video title
	BvID         string    // Bilibili video ID (for URL generation)
	PlayCount    int64     // view/play count
	CommentCount int64     // comment count
	LikeCount    int64     // like count
	Duration     int       // duration in seconds
	PubDate      time.Time // publish date
}

// AuthorStats holds computed statistics for an author's videos.
// Calculated by stats.CalcAuthorStats() from []VideoDetail.
type AuthorStats struct {
	AvgPlayCount    float64 // average play count across all videos
	AvgDuration     float64 // average duration in seconds
	AvgCommentCount float64 // average comment count
}

// TopVideo represents a top-performing video (for CSV HYPERLINK output).
type TopVideo struct {
	Title     string // video title
	URL       string // full video URL
	PlayCount int64  // play count (used for sorting)
}

// Author represents a content creator with aggregated stats (stage 1 output).
// Assembled in RunStage1 from AuthorInfo + stats.CalcAuthorStats() results.
type Author struct {
	Name           string      // author display name
	ID             string      // platform-specific author ID
	Followers      int64       // follower count
	VideoCount     int         // total video count (from API metadata, not len(videos))
	TotalLikes     int64       // total likes across all content
	TotalPlayCount int64       // total play count across all videos
	Stats          AuthorStats // aggregated statistics
	TopVideos      []TopVideo  // top 3 videos by play count
}
