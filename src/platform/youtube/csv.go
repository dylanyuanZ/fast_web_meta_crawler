package youtube

import (
	"fmt"
	"strings"
)

// VideoHeader returns the CSV header for YouTube video search results.
func VideoHeader() []string {
	return []string{
		"标题", "作者", "ChannelID", "VideoID", "播放次数", "发布时间", "视频时长(s)", "说明",
	}
}

// VideoToRow converts a YouTube Video to a CSV row matching VideoHeader columns.
func VideoToRow(v Video) []string {
	return []string{
		v.Title,
		v.Author,
		v.ChannelID,
		v.VideoID,
		fmt.Sprintf("%d", v.PlayCount),
		v.PubDate.Format("2006-01-02 15:04:05"),
		fmt.Sprintf("%d", v.Duration),
		v.Description,
	}
}

// VideoAuthorNameCol is the column index for author name in video CSV.
const VideoAuthorNameCol = 1

// VideoAuthorIDCol is the column index for channel ID in video CSV.
const VideoAuthorIDCol = 2

// AuthorHeader returns the CSV header for YouTube author info (Stage 1).
func AuthorHeader() []string {
	return []string{
		"频道名称", "ChannelID", "Handle", "粉丝数", "总播放数", "视频数量",
		"注册时间", "地区", "说明", "外部链接",
	}
}

// AuthorInfoToRow converts a YouTube AuthorInfo to a CSV row matching AuthorHeader columns.
func AuthorInfoToRow(info *AuthorInfo) []string {
	joinDate := ""
	if !info.JoinDate.IsZero() {
		joinDate = info.JoinDate.Format("2006-01-02")
	}
	return []string{
		info.Name,
		info.ChannelID,
		info.Handle,
		fmt.Sprintf("%d", info.Followers),
		fmt.Sprintf("%d", info.TotalPlayCount),
		fmt.Sprintf("%d", info.VideoCount),
		joinDate,
		info.Region,
		info.Description,
		strings.Join(info.ExternalLinks, "\n"),
	}
}
