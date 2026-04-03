package bilibili

import (
	"fmt"
	"strings"
)

// VideoURLPrefix is the base URL for Bilibili video pages.
const VideoURLPrefix = "https://www.bilibili.com/video/"

// safeDiv returns a/b, or 0 if b is 0. Prevents division by zero for authors with no videos.
func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// topVideoHyperlink generates an Excel HYPERLINK formula for the i-th top video.
// Returns empty string if index is out of range.
func topVideoHyperlink(topVideos []TopVideo, index int) string {
	if index >= len(topVideos) {
		return ""
	}
	v := topVideos[index]
	title := strings.ReplaceAll(v.Title, "\"", "\"\"")
	return fmt.Sprintf(`=HYPERLINK("%s","%s")`, v.URL, title)
}

// TopVideo represents a top-performing video (for CSV HYPERLINK output).
// Defined here to avoid circular imports with src package.
type TopVideo struct {
	Title     string
	URL       string
	PlayCount int64
}
