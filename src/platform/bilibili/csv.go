package bilibili

// VideoURLPrefix is the base URL for Bilibili video pages.
const VideoURLPrefix = "https://www.bilibili.com/video/"

// safeDiv returns a/b, or 0 if b is 0. Prevents division by zero for authors with no videos.
func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}
