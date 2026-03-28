package stats

import (
	"fmt"
	"slices"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
)

// CalcAuthorStats computes aggregated statistics and top-N videos from a list of video details.
// topN specifies how many top videos to return (sorted by play count descending).
// videoURLPrefix is the platform-specific base URL for video pages (e.g. "https://www.bilibili.com/video/").
// Returns zero-value AuthorStats and nil TopVideos if videos is empty.
// Does not modify the original slice.
func CalcAuthorStats(videos []src.VideoDetail, topN int, videoURLPrefix string) (src.AuthorStats, []src.TopVideo) {
	if len(videos) == 0 {
		return src.AuthorStats{}, nil
	}

	var totalPlay, totalComment int64
	var totalDuration int

	for _, v := range videos {
		totalPlay += v.PlayCount
		totalDuration += v.Duration
		totalComment += v.CommentCount
	}

	count := float64(len(videos))
	stats := src.AuthorStats{
		AvgPlayCount:    float64(totalPlay) / count,
		AvgDuration:     float64(totalDuration) / count,
		AvgCommentCount: float64(totalComment) / count,
	}

	// Sort a copy to avoid mutating the original slice.
	sorted := make([]src.VideoDetail, len(videos))
	copy(sorted, videos)
	slices.SortFunc(sorted, func(a, b src.VideoDetail) int {
		if a.PlayCount > b.PlayCount {
			return -1
		}
		if a.PlayCount < b.PlayCount {
			return 1
		}
		return 0
	})

	n := topN
	if n > len(sorted) {
		n = len(sorted)
	}

	topVideos := make([]src.TopVideo, n)
	for i := 0; i < n; i++ {
		topVideos[i] = src.TopVideo{
			Title:     sorted[i].Title,
			URL:       fmt.Sprintf("%s%s", videoURLPrefix, sorted[i].BvID),
			PlayCount: sorted[i].PlayCount,
		}
	}

	return stats, topVideos
}
