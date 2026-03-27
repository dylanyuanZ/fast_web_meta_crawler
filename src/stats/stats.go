package stats

import (
	"fmt"
	"slices"
	"strings"

	lingua "github.com/pemistahl/lingua-go"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
)

// detector is the global lingua language detector, initialized once via InitDetector().
var detector lingua.LanguageDetector

// bilibiliVideoURLPrefix is the base URL for Bilibili video pages.
const bilibiliVideoURLPrefix = "https://www.bilibili.com/video/"

// InitDetector initializes the global lingua-go language detector with 17 candidate languages.
// Must be called once at startup before any DetectLanguage calls.
func InitDetector() {
	languages := []lingua.Language{
		lingua.Chinese, lingua.Japanese, lingua.Korean,
		lingua.English, lingua.German, lingua.Spanish,
		lingua.French, lingua.Portuguese, lingua.Russian,
		lingua.Arabic, lingua.Thai, lingua.Vietnamese,
		lingua.Italian, lingua.Dutch, lingua.Turkish,
		lingua.Indonesian, lingua.Hindi,
	}
	detector = lingua.NewLanguageDetectorBuilder().
		FromLanguages(languages...).
		WithMinimumRelativeDistance(0.25).
		Build()
}

// CalcAuthorStats computes aggregated statistics and top-N videos from a list of video details.
// topN specifies how many top videos to return (sorted by play count descending).
// Returns zero-value AuthorStats and nil TopVideos if videos is empty.
// Does not modify the original slice.
func CalcAuthorStats(videos []src.VideoDetail, topN int) (src.AuthorStats, []src.TopVideo) {
	if len(videos) == 0 {
		return src.AuthorStats{}, nil
	}

	var totalPlay, totalComment, totalLike int64
	var totalDuration int

	for _, v := range videos {
		totalPlay += v.PlayCount
		totalDuration += v.Duration
		totalComment += v.CommentCount
		totalLike += v.LikeCount
	}

	count := float64(len(videos))
	stats := src.AuthorStats{
		AvgPlayCount:    float64(totalPlay) / count,
		AvgDuration:     float64(totalDuration) / count,
		AvgCommentCount: float64(totalComment) / count,
		AvgLikeCount:    float64(totalLike) / count,
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
			URL:       fmt.Sprintf("%s%s", bilibiliVideoURLPrefix, sorted[i].BvID),
			PlayCount: sorted[i].PlayCount,
		}
	}

	return stats, topVideos
}

// DetectLanguage detects the dominant language from a list of video titles.
// Concatenates all titles into a single text, then uses lingua-go for detection.
// Returns ISO 639-1 language code (e.g. "zh", "en", "ja") or "unknown".
func DetectLanguage(titles []string) string {
	if len(titles) == 0 {
		return "unknown"
	}

	text := strings.Join(titles, " ")
	if strings.TrimSpace(text) == "" {
		return "unknown"
	}

	lang, exists := detector.DetectLanguageOf(text)
	if !exists {
		return "unknown"
	}

	return lang.IsoCode639_1().String()
}
