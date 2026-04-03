package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
)

// YouTubeSearchRecorder implements src.SearchRecorder for YouTube.
type YouTubeSearchRecorder struct {
	manager *browser.Manager
}

// Compile-time interface check.
var _ src.SearchRecorder = (*YouTubeSearchRecorder)(nil)

// NewSearchRecorder creates a new YouTubeSearchRecorder.
func NewSearchRecorder(manager *browser.Manager) *YouTubeSearchRecorder {
	return &YouTubeSearchRecorder{manager: manager}
}

// ytInitialDataExtractJS extracts ytInitialData from YouTube's SSR page.
const ytInitialDataExtractJS = `() => {
	if (window.ytInitialData) {
		return JSON.stringify(window.ytInitialData);
	}
	return '';
}`

// SearchAndRecord implements src.SearchRecorder for YouTube.
// It navigates to the YouTube search page, extracts initial results from SSR data,
// then scrolls to load more results until no more are available or max scroll count is reached.
func (s *YouTubeSearchRecorder) SearchAndRecord(ctx context.Context, keyword string, csvWriter src.CSVRowWriter, progress src.ProgressTracker) (int, error) {
	cfg := config.Get()
	ytCfg := cfg.Platform.YouTube

	p := s.manager.GetPage()
	defer s.manager.PutPage(p)

	// Build search URL with filter parameters.
	searchURL := buildSearchURL(keyword, ytCfg)
	log.Printf("INFO: [youtube] Navigating to search URL: %s", searchURL)

	// Navigate and extract ytInitialData.
	rawJSON, err := browser.NavigateAndExtract(ctx, p, searchURL, ytInitialDataExtractJS)
	if err != nil {
		return 0, fmt.Errorf("youtube search: %w", err)
	}

	if rawJSON == "" {
		return 0, fmt.Errorf("youtube search: ytInitialData not found")
	}

	// Parse initial data.
	videos, hasContinuation := parseSearchResults(rawJSON)
	totalVideos := len(videos)

	// Truncate initial results if they exceed the limit.
	if len(videos) > cfg.MaxSearchVideos {
		log.Printf("INFO: [youtube] Initial results (%d) exceed max_search_videos (%d), truncating", len(videos), cfg.MaxSearchVideos)
		videos = videos[:cfg.MaxSearchVideos]
		totalVideos = len(videos)
	}

	// Write initial results to CSV.
	if len(videos) > 0 {
		rows := videosToRows(videos)
		if err := csvWriter.WriteRows(rows); err != nil {
			return 0, fmt.Errorf("write initial videos to CSV: %w", err)
		}
	}

	log.Printf("INFO: [youtube] Initial search results: %d videos, hasContinuation=%v", totalVideos, hasContinuation)

	// Check if initial results already meet the limit.
	if totalVideos >= cfg.MaxSearchVideos {
		log.Printf("INFO: [youtube] Reached max_search_videos (%d) from initial results, stopping", cfg.MaxSearchVideos)
		return totalVideos, nil
	}

	// Scroll to load more results.
	scrollCount := 0
	for hasContinuation {
		if ctx.Err() != nil {
			break
		}
		if totalVideos >= cfg.MaxSearchVideos {
			log.Printf("INFO: [youtube] Reached max_search_videos (%d), stopping", cfg.MaxSearchVideos)
			break
		}

		// Scroll to bottom.
		_, err := p.Eval(`() => { window.scrollTo(0, document.documentElement.scrollHeight); }`)
		if err != nil {
			log.Printf("WARN: [youtube] scroll failed: %v", err)
			break
		}

		// Wait for new content to load.
		time.Sleep(cfg.GetPlatformRequestInterval("youtube"))
		scrollCount++

		// Check for new content by re-extracting ytInitialData.
		// YouTube dynamically updates the DOM, so we check for continuation items.
		hasContinuationJS := `() => {
			const items = document.querySelectorAll('ytd-continuation-item-renderer');
			return items.length > 0 ? 'true' : 'false';
		}`
		result, err := p.Eval(hasContinuationJS)
		if err != nil {
			log.Printf("WARN: [youtube] check continuation failed: %v", err)
			break
		}

		hasContinuation = result.Value.String() == "true"

		// Extract newly loaded video items from DOM.
		// Note: DOM extraction is less reliable than SSR parsing. We extract
		// videoId from href (stripping query params), and use aria-label for
		// views/publishTime as a fallback since metadata-line spans can vary.
		newVideosJS := `() => {
			const items = document.querySelectorAll('ytd-video-renderer');
			const videos = [];
			items.forEach(item => {
				const titleEl = item.querySelector('#video-title');
				const channelEl = item.querySelector('#channel-name a');
				const durationEl = item.querySelector('ytd-thumbnail-overlay-time-status-renderer span');
				
				// Extract view count and publish time from metadata line.
				// YouTube renders these as inline spans inside #metadata-line.
				const metaSpans = item.querySelectorAll('#metadata-line span.inline-metadata-item');
				let views = '';
				let publishTime = '';
				if (metaSpans.length >= 1) views = metaSpans[0].textContent.trim();
				if (metaSpans.length >= 2) publishTime = metaSpans[1].textContent.trim();
				
				if (titleEl) {
					// Extract clean videoId from href: "/watch?v=XXX&pp=YYY" → "XXX"
					let videoId = '';
					const href = titleEl.getAttribute('href') || '';
					const match = href.match(/[?&]v=([^&]+)/);
					if (match) videoId = match[1];
					
					// Extract channel ID: prefer data-* attributes, fallback to URL.
					let channelUrl = '';
					if (channelEl) channelUrl = channelEl.getAttribute('href') || '';
					
					videos.push({
						title: titleEl.textContent.trim(),
						videoId: videoId,
						author: channelEl ? channelEl.textContent.trim() : '',
						channelUrl: channelUrl,
						views: views,
						publishTime: publishTime,
						duration: durationEl ? durationEl.textContent.trim() : ''
					});
				}
			});
			return JSON.stringify(videos);
		}`
		domResult, err := p.Eval(newVideosJS)
		if err != nil {
			log.Printf("WARN: [youtube] extract DOM videos failed: %v", err)
			break
		}

		var domVideos []struct {
			Title       string `json:"title"`
			VideoID     string `json:"videoId"`
			Author      string `json:"author"`
			ChannelURL  string `json:"channelUrl"`
			Views       string `json:"views"`
			PublishTime string `json:"publishTime"`
			Duration    string `json:"duration"`
		}
		if err := json.Unmarshal([]byte(domResult.Value.String()), &domVideos); err != nil {
			log.Printf("WARN: [youtube] parse DOM videos failed: %v", err)
			break
		}

		// Only process videos beyond what we've already seen.
		if len(domVideos) > totalVideos {
			newCount := len(domVideos) - totalVideos

			// Truncate to remaining quota so we never exceed max_search_videos.
			remaining := cfg.MaxSearchVideos - totalVideos
			if newCount > remaining {
				newCount = remaining
			}

			newVideos := make([]Video, 0, newCount)
			for i := totalVideos; i < totalVideos+newCount; i++ {
				dv := domVideos[i]
				channelID := extractChannelID(dv.ChannelURL)
				newVideos = append(newVideos, Video{
					Title:     dv.Title,
					Author:    dv.Author,
					ChannelID: channelID,
					VideoID:   dv.VideoID,
					PlayCount: parseViewCount(dv.Views),
					Duration:  parseDurationString(dv.Duration),
				})
			}

			if len(newVideos) > 0 {
				rows := videosToRows(newVideos)
				if err := csvWriter.WriteRows(rows); err != nil {
					log.Printf("WARN: [youtube] write scroll videos to CSV: %v", err)
				}
				totalVideos += len(newVideos)
				log.Printf("INFO: [youtube] Scroll %d: +%d videos (total: %d)", scrollCount, len(newVideos), totalVideos)
			}
		}
	}

	log.Printf("INFO: [youtube] Search completed: %d total videos, %d scrolls", totalVideos, scrollCount)
	return totalVideos, nil
}

// videosToRows converts a slice of YouTube Videos to CSV rows.
func videosToRows(videos []Video) [][]string {
	rows := make([][]string, 0, len(videos))
	for _, v := range videos {
		rows = append(rows, VideoToRow(v))
	}
	return rows
}

// buildSearchURL constructs a YouTube search URL with filter parameters.
func buildSearchURL(keyword string, ytCfg config.YouTubeConfig) string {
	baseURL := fmt.Sprintf("https://www.youtube.com/results?search_query=%s", keyword)

	sp := buildSPParam(ytCfg)
	if sp != "" {
		baseURL += "&sp=" + sp
	}

	return baseURL
}

// buildSPParam constructs the YouTube search filter parameter (sp=).
// YouTube uses a base64-encoded protobuf for search filters.
// For simplicity, we use known sp values for common filter combinations.
//
// Sort priority: when a non-default sort is configured, it takes precedence over filters
// because YouTube's sp parameter is a protobuf encoding where simple concatenation is not possible.
func buildSPParam(ytCfg config.YouTubeConfig) string {
	// Step 1: Check sort configuration first (sort takes priority over filters).
	// YouTube search page only supports "relevance" (default, no sp param) and "popularity" (sp=CAM%3D).
	sortBy := strings.ToLower(strings.TrimSpace(ytCfg.SearchPageSortBy))
	if sortBy != "" && sortBy != "relevance" {
		switch sortBy {
		case "popularity":
			return "CAM%3D"
		default:
			log.Printf("WARN: [youtube] unknown search_page_sort_by value: %q, ignoring", ytCfg.SearchPageSortBy)
		}
	}

	// Step 2: Apply filter parameters (only when no sort override).
	// Common sp values for YouTube search filters.
	// These are pre-computed base64-encoded protobuf values.
	switch {
	case ytCfg.FilterType == "video" && ytCfg.FilterDuration == "" && ytCfg.FilterUpload == "":
		return "EgIQAQ%3D%3D" // Type: Video
	case ytCfg.FilterType == "short":
		return "EgIYAQ%3D%3D" // Type: Short
	case ytCfg.FilterDuration == "short":
		return "EgIYAQ%3D%3D" // Duration: Under 4 minutes
	case ytCfg.FilterDuration == "medium":
		return "EgIYAw%3D%3D" // Duration: 4-20 minutes
	case ytCfg.FilterDuration == "long":
		return "EgIYAg%3D%3D" // Duration: Over 20 minutes
	case ytCfg.FilterUpload == "today":
		return "CAISBAgBEAE%3D" // Upload date: Today + Video
	case ytCfg.FilterUpload == "week":
		return "CAISBAgCEAE%3D" // Upload date: This week + Video
	case ytCfg.FilterUpload == "month":
		return "CAISBAgDEAE%3D" // Upload date: This month + Video
	case ytCfg.FilterUpload == "year":
		return "CAISBAgFEAE%3D" // Upload date: This year + Video
	default:
		return ""
	}
}

// parseSearchResults parses ytInitialData JSON to extract video results.
// Returns the list of videos and whether there are more results (continuation).
func parseSearchResults(rawJSON string) ([]Video, bool) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &data); err != nil {
		log.Printf("WARN: [youtube] failed to parse ytInitialData: %v", err)
		return nil, false
	}

	// Navigate the nested structure to find video renderers.
	// ytInitialData.contents.twoColumnSearchResultsRenderer.primaryContents.sectionListRenderer.contents[0].itemSectionRenderer.contents
	contents, ok := navigateJSON(data,
		"contents", "twoColumnSearchResultsRenderer", "primaryContents",
		"sectionListRenderer", "contents")
	if !ok {
		log.Printf("WARN: [youtube] could not find search results in ytInitialData")
		return nil, false
	}

	contentsList, ok := contents.([]interface{})
	if !ok || len(contentsList) == 0 {
		return nil, false
	}

	var videos []Video
	hasContinuation := false

	for _, section := range contentsList {
		sectionMap, ok := section.(map[string]interface{})
		if !ok {
			continue
		}

		// Check for continuation item.
		if _, ok := sectionMap["continuationItemRenderer"]; ok {
			hasContinuation = true
			continue
		}

		// Process item section.
		itemSection, ok := sectionMap["itemSectionRenderer"]
		if !ok {
			continue
		}
		itemSectionMap, ok := itemSection.(map[string]interface{})
		if !ok {
			continue
		}
		items, ok := itemSectionMap["contents"]
		if !ok {
			continue
		}
		itemsList, ok := items.([]interface{})
		if !ok {
			continue
		}

		for _, item := range itemsList {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			videoRenderer, ok := itemMap["videoRenderer"]
			if !ok {
				continue
			}
			vrMap, ok := videoRenderer.(map[string]interface{})
			if !ok {
				continue
			}

			video := extractVideoFromRenderer(vrMap)
			if video.VideoID != "" {
				videos = append(videos, video)
			}
		}
	}

	return videos, hasContinuation
}

// extractVideoFromRenderer extracts a Video from a videoRenderer JSON object.
func extractVideoFromRenderer(vr map[string]interface{}) Video {
	video := Video{}

	// Video ID.
	if id, ok := vr["videoId"].(string); ok {
		video.VideoID = id
	}

	// Title.
	if title, ok := navigateJSON(vr, "title", "runs"); ok {
		if runs, ok := title.([]interface{}); ok && len(runs) > 0 {
			if run, ok := runs[0].(map[string]interface{}); ok {
				if text, ok := run["text"].(string); ok {
					video.Title = text
				}
			}
		}
	}

	// Author and Channel ID.
	if channel, ok := navigateJSON(vr, "ownerText", "runs"); ok {
		if runs, ok := channel.([]interface{}); ok && len(runs) > 0 {
			if run, ok := runs[0].(map[string]interface{}); ok {
				if text, ok := run["text"].(string); ok {
					video.Author = text
				}
				if navEndpoint, ok := run["navigationEndpoint"].(map[string]interface{}); ok {
					if browseEndpoint, ok := navEndpoint["browseEndpoint"].(map[string]interface{}); ok {
						if browseID, ok := browseEndpoint["browseId"].(string); ok {
							video.ChannelID = browseID
						}
					}
				}
			}
		}
	}

	// View count.
	if viewCount, ok := navigateJSON(vr, "viewCountText", "simpleText"); ok {
		if text, ok := viewCount.(string); ok {
			video.PlayCount = parseViewCount(text)
		}
	}

	// Duration.
	if duration, ok := navigateJSON(vr, "lengthText", "simpleText"); ok {
		if text, ok := duration.(string); ok {
			video.Duration = parseDurationString(text)
		}
	}

	// Description snippet.
	if descSnippet, ok := navigateJSON(vr, "detailedMetadataSnippets"); ok {
		if snippets, ok := descSnippet.([]interface{}); ok && len(snippets) > 0 {
			if snippet, ok := snippets[0].(map[string]interface{}); ok {
				if snippetText, ok := snippet["snippetText"].(map[string]interface{}); ok {
					if runs, ok := snippetText["runs"].([]interface{}); ok {
						var parts []string
						for _, run := range runs {
							if runMap, ok := run.(map[string]interface{}); ok {
								if text, ok := runMap["text"].(string); ok {
									parts = append(parts, text)
								}
							}
						}
						video.Description = strings.Join(parts, "")
					}
				}
			}
		}
	}

	// Publish time (relative, e.g. "3 days ago").
	if publishedTime, ok := navigateJSON(vr, "publishedTimeText", "simpleText"); ok {
		if text, ok := publishedTime.(string); ok {
			video.PubDate = parseRelativeTime(text)
		}
	}

	return video
}

// navigateJSON navigates a nested JSON structure by keys.
func navigateJSON(data interface{}, keys ...string) (interface{}, bool) {
	current := data
	for _, key := range keys {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// extractChannelID extracts channel ID from a channel URL like "/@username" or "/channel/UCxxxxxx".
func extractChannelID(channelURL string) string {
	if strings.HasPrefix(channelURL, "/channel/") {
		return strings.TrimPrefix(channelURL, "/channel/")
	}
	if strings.HasPrefix(channelURL, "/@") {
		return strings.TrimPrefix(channelURL, "/")
	}
	return channelURL
}

// viewCountRegex matches numbers in view count strings like "1,234 views" or "1.2M views".
var viewCountRegex = regexp.MustCompile(`[\d,]+`)

// parseViewCount parses a view count string like "1,234 views" or "1.2M views" to int64.
func parseViewCount(s string) int64 {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")

	// Handle abbreviated counts (e.g. "1.2M", "500K").
	multiplier := int64(1)
	sLower := strings.ToLower(s)
	if strings.Contains(sLower, "k") {
		multiplier = 1000
		s = strings.Split(sLower, "k")[0]
	} else if strings.Contains(sLower, "m") {
		multiplier = 1000000
		s = strings.Split(sLower, "m")[0]
	} else if strings.Contains(sLower, "b") {
		multiplier = 1000000000
		s = strings.Split(sLower, "b")[0]
	}

	// Extract numeric part.
	matches := viewCountRegex.FindString(s)
	if matches == "" {
		return 0
	}

	if strings.Contains(matches, ".") {
		f, err := strconv.ParseFloat(matches, 64)
		if err != nil {
			return 0
		}
		return int64(f * float64(multiplier))
	}

	n, err := strconv.ParseInt(matches, 10, 64)
	if err != nil {
		return 0
	}
	return n * multiplier
}

// parseDurationString parses a duration string like "12:34" or "1:02:03" to seconds.
func parseDurationString(s string) int {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	total := 0

	switch len(parts) {
	case 2: // mm:ss
		m, _ := strconv.Atoi(parts[0])
		sec, _ := strconv.Atoi(parts[1])
		total = m*60 + sec
	case 3: // hh:mm:ss
		h, _ := strconv.Atoi(parts[0])
		m, _ := strconv.Atoi(parts[1])
		sec, _ := strconv.Atoi(parts[2])
		total = h*3600 + m*60 + sec
	default:
		n, _ := strconv.Atoi(s)
		total = n
	}

	return total
}

// parseRelativeTime parses a relative time string like "3 days ago" to an approximate time.Time.
func parseRelativeTime(s string) time.Time {
	s = strings.ToLower(strings.TrimSpace(s))
	now := time.Now()

	// Common patterns: "X hours ago", "X days ago", "X weeks ago", "X months ago", "X years ago"
	parts := strings.Fields(s)
	if len(parts) < 2 {
		return time.Time{}
	}

	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return time.Time{}
	}

	unit := parts[1]
	switch {
	case strings.HasPrefix(unit, "second"):
		return now.Add(-time.Duration(n) * time.Second)
	case strings.HasPrefix(unit, "minute"):
		return now.Add(-time.Duration(n) * time.Minute)
	case strings.HasPrefix(unit, "hour"):
		return now.Add(-time.Duration(n) * time.Hour)
	case strings.HasPrefix(unit, "day"):
		return now.AddDate(0, 0, -n)
	case strings.HasPrefix(unit, "week"):
		return now.AddDate(0, 0, -n*7)
	case strings.HasPrefix(unit, "month"):
		return now.AddDate(0, -n, 0)
	case strings.HasPrefix(unit, "year"):
		return now.AddDate(-n, 0, 0)
	default:
		return time.Time{}
	}
}
