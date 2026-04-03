package youtube

import "time"

// Video represents a single video from YouTube search results (stage 0 output).
type Video struct {
	Title       string    // video title
	Author      string    // channel display name
	ChannelID   string    // YouTube channel ID (e.g. UCxxxxxx)
	VideoID     string    // YouTube video ID (e.g. dQw4w9WgXcQ)
	Description string    // video description snippet
	PlayCount   int64     // view count
	PubDate     time.Time // publish date
	Duration    int       // duration in seconds
}

// AuthorInfo holds YouTube channel information (stage 1 output).
type AuthorInfo struct {
	Name           string    // channel display name
	Handle         string    // channel handle (e.g. @username)
	ChannelID      string    // YouTube channel ID
	Description    string    // channel description
	Region         string    // channel region/country
	Followers      int64     // subscriber count
	TotalPlayCount int64     // total view count across all videos
	VideoCount     int       // total video count
	JoinDate       time.Time // channel creation date
	ExternalLinks  []string  // external links from channel description
}
