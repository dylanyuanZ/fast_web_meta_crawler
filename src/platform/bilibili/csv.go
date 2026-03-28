package bilibili

import (
	"fmt"
	"strings"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
)

// VideoURLPrefix is the base URL for Bilibili video pages.
// Exported so that callers (e.g. main.go) can inject it into CalcAuthorStats.
const VideoURLPrefix = "https://www.bilibili.com/video/"

// BilibiliAuthorCSVAdapter implements src.AuthorCSVAdapter for Bilibili platform.
type BilibiliAuthorCSVAdapter struct{}

// Compile-time interface checks.
var _ src.AuthorCSVAdapter = (*BilibiliAuthorCSVAdapter)(nil)

// BasicHeader returns the 8-column CSV header for Stage 1 (basic author info).
func (a *BilibiliAuthorCSVAdapter) BasicHeader() []string {
	return []string{
		"博主名字", "ID", "粉丝数", "总获赞数", "总播放数", "视频数量",
		"视频平均播放量", "视频平均点赞量",
	}
}

// BasicRow converts an Author to a CSV row matching BasicHeader columns.
// Average play/like counts are computed from API summary data (TotalPlayCount/VideoCount).
func (a *BilibiliAuthorCSVAdapter) BasicRow(author src.Author) []string {
	avgPlay := safeDiv(float64(author.TotalPlayCount), float64(author.VideoCount))
	avgLike := safeDiv(float64(author.TotalLikes), float64(author.VideoCount))
	return []string{
		author.Name,
		author.ID,
		fmt.Sprintf("%d", author.Followers),
		fmt.Sprintf("%d", author.TotalLikes),
		fmt.Sprintf("%d", author.TotalPlayCount),
		fmt.Sprintf("%d", author.VideoCount),
		fmt.Sprintf("%.1f", avgPlay),
		fmt.Sprintf("%.1f", avgLike),
	}
}

// FullHeader returns the 13-column CSV header for Stage 2 (full author info with video stats).
func (a *BilibiliAuthorCSVAdapter) FullHeader() []string {
	return []string{
		"博主名字", "ID", "粉丝数", "总获赞数", "总播放数", "视频数量",
		"视频平均播放量", "视频平均点赞量",
		"视频平均评论数", "视频平均时长",
		"视频_TOP1", "视频_TOP2", "视频_TOP3",
	}
}

// FullRow converts an Author to a CSV row matching FullHeader columns.
// Includes stats from video traversal (avg comment, avg duration, TOP3).
func (a *BilibiliAuthorCSVAdapter) FullRow(author src.Author) []string {
	avgPlay := safeDiv(float64(author.TotalPlayCount), float64(author.VideoCount))
	avgLike := safeDiv(float64(author.TotalLikes), float64(author.VideoCount))
	return []string{
		author.Name,
		author.ID,
		fmt.Sprintf("%d", author.Followers),
		fmt.Sprintf("%d", author.TotalLikes),
		fmt.Sprintf("%d", author.TotalPlayCount),
		fmt.Sprintf("%d", author.VideoCount),
		fmt.Sprintf("%.1f", avgPlay),
		fmt.Sprintf("%.1f", avgLike),
		fmt.Sprintf("%.1f", author.Stats.AvgCommentCount),
		fmt.Sprintf("%.1f", author.Stats.AvgDuration),
		topVideoHyperlink(author.TopVideos, 0),
		topVideoHyperlink(author.TopVideos, 1),
		topVideoHyperlink(author.TopVideos, 2),
	}
}

// BilibiliVideoCSVAdapter implements src.VideoCSVAdapter for Bilibili platform.
type BilibiliVideoCSVAdapter struct{}

// Compile-time interface check.
var _ src.VideoCSVAdapter = (*BilibiliVideoCSVAdapter)(nil)

// Header returns the CSV header for video data.
func (a *BilibiliVideoCSVAdapter) Header() []string {
	return []string{
		"标题", "作者", "AuthorID", "播放次数", "发布时间", "视频时长(s)", "来源",
	}
}

// Row converts a Video to a CSV row matching Header columns.
func (a *BilibiliVideoCSVAdapter) Row(video src.Video) []string {
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

// safeDiv returns a/b, or 0 if b is 0. Prevents division by zero for authors with no videos.
func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// topVideoHyperlink generates an Excel HYPERLINK formula for the i-th top video.
// Returns empty string if index is out of range.
// Migrated from export package — platform-specific URL format belongs here.
func topVideoHyperlink(topVideos []src.TopVideo, index int) string {
	if index >= len(topVideos) {
		return ""
	}
	v := topVideos[index]
	title := strings.ReplaceAll(v.Title, "\"", "\"\"")
	return fmt.Sprintf(`=HYPERLINK("%s","%s")`, v.URL, title)
}
