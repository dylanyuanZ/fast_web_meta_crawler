package export

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
)

// GenerateFileName creates a CSV filename in the format: {platform}_{keyword}_{date}_{time}_{type}.csv
func GenerateFileName(platform, keyword, fileType string) string {
	now := time.Now()
	return fmt.Sprintf("%s_%s_%s_%s.csv",
		platform,
		keyword,
		now.Format("20060102"),
		now.Format("150405")+"_"+fileType,
	)
}

// WriteVideoCSV writes a list of videos to a CSV file in the output directory.
// Returns the full path of the created file.
func WriteVideoCSV(outputDir string, videos []src.Video, platform, keyword string) (string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	filename := GenerateFileName(platform, keyword, "videos")
	fullPath := filepath.Join(outputDir, filename)

	f, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	// Write UTF-8 BOM for Excel compatibility.
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return "", fmt.Errorf("write BOM: %w", err)
	}

	w := csv.NewWriter(f)
	defer w.Flush()

	// Write header.
	header := []string{"标题", "作者", "播放次数", "发布时间", "视频时长(s)", "来源"}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("write header: %w", err)
	}

	// Write data rows.
	for _, v := range videos {
		row := []string{
			v.Title,
			v.Author,
			fmt.Sprintf("%d", v.PlayCount),
			v.PubDate.Format("2006-01-02 15:04:05"),
			fmt.Sprintf("%d", v.Duration),
			v.Source,
		}
		if err := w.Write(row); err != nil {
			return "", fmt.Errorf("write row: %w", err)
		}
	}

	return fullPath, nil
}

// WriteAuthorCSV writes a list of authors to a CSV file in the output directory.
// TOP video columns use Excel HYPERLINK formula for clickable links.
// Returns the full path of the created file.
func WriteAuthorCSV(outputDir string, authors []src.Author, platform, keyword string) (string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	filename := GenerateFileName(platform, keyword, "authors")
	fullPath := filepath.Join(outputDir, filename)

	f, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	// Write UTF-8 BOM for Excel compatibility.
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return "", fmt.Errorf("write BOM: %w", err)
	}

	w := csv.NewWriter(f)
	defer w.Flush()

	// Write header.
	header := []string{
		"博主名字", "粉丝数", "视频数量",
		"视频平均播放量", "视频平均时长", "视频平均评论数", "视频平均点赞量",
		"地区", "语言",
		"视频_TOP1", "视频_TOP2", "视频_TOP3",
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("write header: %w", err)
	}

	// Write data rows.
	for _, a := range authors {
		row := []string{
			a.Name,
			fmt.Sprintf("%d", a.Followers),
			fmt.Sprintf("%d", a.VideoCount),
			fmt.Sprintf("%.1f", a.Stats.AvgPlayCount),
			fmt.Sprintf("%.1f", a.Stats.AvgDuration),
			fmt.Sprintf("%.1f", a.Stats.AvgCommentCount),
			fmt.Sprintf("%.1f", a.Stats.AvgLikeCount),
			a.Region,
			a.Language,
			topVideoHyperlink(a.TopVideos, 0),
			topVideoHyperlink(a.TopVideos, 1),
			topVideoHyperlink(a.TopVideos, 2),
		}
		if err := w.Write(row); err != nil {
			return "", fmt.Errorf("write row: %w", err)
		}
	}

	return fullPath, nil
}

// topVideoHyperlink generates an Excel HYPERLINK formula for the i-th top video.
// Returns empty string if index is out of range.
func topVideoHyperlink(topVideos []src.TopVideo, index int) string {
	if index >= len(topVideos) {
		return ""
	}
	v := topVideos[index]
	// Escape double quotes in title for Excel formula.
	title := strings.ReplaceAll(v.Title, "\"", "\"\"")
	return fmt.Sprintf(`=HYPERLINK("%s","%s")`, v.URL, title)
}
