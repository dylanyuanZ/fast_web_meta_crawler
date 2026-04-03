package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// YouTubeAuthorCrawler implements src.AuthorCrawler for YouTube.
type YouTubeAuthorCrawler struct {
	manager *browser.Manager
}

// Compile-time interface check.
var _ src.AuthorCrawler = (*YouTubeAuthorCrawler)(nil)

// NewAuthorCrawler creates a new YouTubeAuthorCrawler.
func NewAuthorCrawler(manager *browser.Manager) *YouTubeAuthorCrawler {
	return &YouTubeAuthorCrawler{manager: manager}
}

// channelPageExtractJS extracts ytInitialData from the YouTube channel page.
const channelPageExtractJS = `() => {
	if (window.ytInitialData) {
		return JSON.stringify(window.ytInitialData);
	}
	return '';
}`

// FetchAuthorInfo implements src.AuthorCrawler.
// It navigates to the YouTube channel page, extracts SSR data for basic info,
// then clicks the description area to trigger the browse API for detailed info
// (join date, total play count, country, external links).
func (c *YouTubeAuthorCrawler) FetchAuthorInfo(ctx context.Context, channelID string) ([]string, error) {
	p := c.manager.GetPage()
	defer c.manager.PutPage(p)

	// Build channel URL — support both channel ID and handle formats.
	var channelURL string
	if strings.HasPrefix(channelID, "@") {
		channelURL = fmt.Sprintf("https://www.youtube.com/%s", channelID)
	} else if strings.HasPrefix(channelID, "UC") {
		channelURL = fmt.Sprintf("https://www.youtube.com/channel/%s", channelID)
	} else {
		channelURL = fmt.Sprintf("https://www.youtube.com/%s", channelID)
	}

	log.Printf("INFO: [youtube] Fetching author info: %s", channelURL)

	// Step 1: Navigate to channel page and extract SSR data.
	rawJSON, err := browser.NavigateAndExtract(ctx, p, channelURL, channelPageExtractJS)
	if err != nil {
		return nil, fmt.Errorf("youtube author info: %w", err)
	}

	if rawJSON == "" {
		return nil, fmt.Errorf("youtube author info: ytInitialData not found for %s", channelID)
	}

	// Step 2: Parse SSR data for basic info (name, description, subscribers, video count).
	info := parseAuthorInfoFromSSR(rawJSON, channelID)

	// Step 3: Try to get detailed info by clicking description area and intercepting browse API.
	enrichAuthorInfoFromBrowseAPI(ctx, p, info)

	return AuthorInfoToRow(info), nil
}

// FetchAllAuthorVideos implements src.AuthorCrawler.
// For YouTube, this is currently a stub that returns an empty list.
func (c *YouTubeAuthorCrawler) FetchAllAuthorVideos(ctx context.Context, channelID string, maxVideos int) ([][]string, error) {
	log.Printf("INFO: [youtube] FetchAllAuthorVideos is a no-op for YouTube (channelID=%s)", channelID)
	return nil, nil
}

// parseAuthorInfoFromSSR parses ytInitialData from a channel page to extract basic AuthorInfo.
// Data sources:
//   - channelMetadataRenderer: name, description, channelID, vanityURL
//   - pageHeaderViewModel.metadata.contentMetadataViewModel.metadataRows: handle, subscribers, video count
func parseAuthorInfoFromSSR(rawJSON string, channelID string) *AuthorInfo {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &data); err != nil {
		log.Printf("WARN: [youtube] failed to parse channel ytInitialData: %v", err)
		return &AuthorInfo{ChannelID: channelID}
	}

	info := &AuthorInfo{ChannelID: channelID}

	// Extract from channelMetadataRenderer (reliable source for name, description, channelID).
	if metadata, ok := navigateJSON(data, "metadata", "channelMetadataRenderer"); ok {
		if metaMap, ok := metadata.(map[string]interface{}); ok {
			if title, ok := metaMap["title"].(string); ok {
				info.Name = title
			}
			if desc, ok := metaMap["description"].(string); ok {
				info.Description = desc
			}
			if externalID, ok := metaMap["externalId"].(string); ok {
				info.ChannelID = externalID
			}
			if vanityURL, ok := metaMap["vanityChannelUrl"].(string); ok {
				parts := strings.Split(vanityURL, "/")
				if len(parts) > 0 {
					info.Handle = parts[len(parts)-1]
				}
			}
		}
	}

	// Extract subscribers and video count from pageHeaderViewModel.
	// Path: header.pageHeaderRenderer.content.pageHeaderViewModel.metadata
	//       .contentMetadataViewModel.metadataRows
	// Row 0: handle (e.g. "@brunomars")
	// Row 1: subscribers (e.g. "43.3M subscribers"), video count (e.g. "121 videos")
	if pageHeader, ok := navigateJSON(data, "header", "pageHeaderRenderer", "content",
		"pageHeaderViewModel", "metadata", "contentMetadataViewModel", "metadataRows"); ok {
		if rows, ok := pageHeader.([]interface{}); ok {
			for i, row := range rows {
				rowMap, ok := row.(map[string]interface{})
				if !ok {
					continue
				}
				parts, ok := rowMap["metadataParts"].([]interface{})
				if !ok {
					continue
				}

				for _, part := range parts {
					partMap, ok := part.(map[string]interface{})
					if !ok {
						continue
					}
					text, ok := navigateJSON(partMap, "text", "content")
					if !ok {
						continue
					}
					textStr, ok := text.(string)
					if !ok {
						continue
					}

					if i == 0 {
						// Row 0: handle.
						if strings.HasPrefix(textStr, "@") {
							info.Handle = textStr
						}
					} else if i == 1 {
						// Row 1: subscribers and video count.
						textLower := strings.ToLower(textStr)
						if strings.Contains(textLower, "subscriber") {
							info.Followers = parseHumanCount(textStr)
						} else if strings.Contains(textLower, "video") {
							info.VideoCount = int(parseHumanCount(textStr))
						}
					}
				}
			}
		}
	}

	return info
}

// enrichAuthorInfoFromBrowseAPI clicks the channel description area to trigger
// the YouTube browse API, then passively intercepts the response to extract detailed info
// from aboutChannelViewModel (join date, total play count, country, external links).
//
// Uses WaitForIntercept (passive NetworkResponseReceived events) instead of HijackRequests
// to avoid modifying the request flow, which could trigger anti-crawl detection.
func enrichAuthorInfoFromBrowseAPI(ctx context.Context, p *rod.Page, info *AuthorInfo) {
	// Set a timeout for this operation.
	enrichCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	pp := p.Context(enrichCtx)

	// Set up passive network listener BEFORE clicking (so we don't miss the response).
	waitFn := browser.WaitForIntercept(enrichCtx, p, []browser.InterceptRule{
		{URLPattern: "youtubei/v1/browse", ID: "about_channel"},
	})

	// Click the description area to trigger the browse API.
	// The selector is "yt-description-preview-view-model" per probe report V2.
	descEl, err := pp.Element("yt-description-preview-view-model")
	if err != nil {
		log.Printf("WARN: [youtube] could not find description preview element for %s: %v", info.ChannelID, err)
		return
	}

	if err := descEl.Click(proto.InputMouseButtonLeft, 1); err != nil {
		log.Printf("WARN: [youtube] failed to click description for %s: %v", info.ChannelID, err)
		return
	}

	// Wait for the browse API response.
	results, err := waitFn()
	if err != nil {
		log.Printf("WARN: [youtube] failed to intercept browse API for %s: %v", info.ChannelID, err)
		return
	}

	// Find the about_channel result and parse it.
	for _, r := range results {
		if r.ID == "about_channel" && len(r.Body) > 0 {
			parseAboutChannelViewModel(string(r.Body), info)
			break
		}
	}
}

// parseAboutChannelViewModel extracts detailed channel info from the browse API response.
// Response structure:
//
//	onResponseReceivedEndpoints[0]
//	  .appendContinuationItemsAction
//	    .continuationItems[0]
//	      .aboutChannelRenderer
//	        .metadata
//	          .aboutChannelViewModel
func parseAboutChannelViewModel(responseBody string, info *AuthorInfo) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(responseBody), &data); err != nil {
		log.Printf("WARN: [youtube] failed to parse browse API response: %v", err)
		return
	}

	// Navigate to aboutChannelViewModel.
	endpoints, ok := data["onResponseReceivedEndpoints"].([]interface{})
	if !ok || len(endpoints) == 0 {
		return
	}

	var viewModel map[string]interface{}
	for _, ep := range endpoints {
		epMap, ok := ep.(map[string]interface{})
		if !ok {
			continue
		}
		action, ok := epMap["appendContinuationItemsAction"].(map[string]interface{})
		if !ok {
			continue
		}
		items, ok := action["continuationItems"].([]interface{})
		if !ok {
			continue
		}
		for _, item := range items {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			renderer, ok := itemMap["aboutChannelRenderer"].(map[string]interface{})
			if !ok {
				continue
			}
			metadata, ok := renderer["metadata"].(map[string]interface{})
			if !ok {
				continue
			}
			vm, ok := metadata["aboutChannelViewModel"].(map[string]interface{})
			if !ok {
				continue
			}
			viewModel = vm
			break
		}
		if viewModel != nil {
			break
		}
	}

	if viewModel == nil {
		log.Printf("WARN: [youtube] aboutChannelViewModel not found in browse API response for %s", info.ChannelID)
		return
	}

	// Extract subscriber count (e.g. "43.3M subscribers").
	if text, ok := viewModel["subscriberCountText"].(string); ok && info.Followers == 0 {
		info.Followers = parseHumanCount(text)
	}

	// Extract video count (e.g. "121 videos").
	if text, ok := viewModel["videoCountText"].(string); ok && info.VideoCount == 0 {
		info.VideoCount = int(parseHumanCount(text))
	}

	// Extract total view count (e.g. "25,113,856,444 views").
	if text, ok := viewModel["viewCountText"].(string); ok {
		info.TotalPlayCount = parseViewCount(text)
	}

	// Extract join date (e.g. "Joined Sep 19, 2006").
	if joinedText, ok := navigateJSON(viewModel, "joinedDateText", "content"); ok {
		if text, ok := joinedText.(string); ok {
			info.JoinDate = parseJoinDate(text)
		}
	}

	// Extract country/region.
	if country, ok := viewModel["country"].(string); ok {
		info.Region = country
	}

	// Extract channel ID (as a fallback).
	if chID, ok := viewModel["channelId"].(string); ok && strings.HasPrefix(chID, "UC") {
		info.ChannelID = chID
	}

	// Extract external links.
	if links, ok := viewModel["links"].([]interface{}); ok {
		for _, link := range links {
			linkMap, ok := link.(map[string]interface{})
			if !ok {
				continue
			}
			// Each link is a channelExternalLinkViewModel.
			linkVM, ok := linkMap["channelExternalLinkViewModel"].(map[string]interface{})
			if !ok {
				continue
			}
			// Extract the actual URL from link.content.
			if linkContent, ok := navigateJSON(linkVM, "link", "content"); ok {
				if url, ok := linkContent.(string); ok && url != "" {
					info.ExternalLinks = append(info.ExternalLinks, ensureHTTPS(url))
				}
			}
		}
	}

	// Extract canonical YouTube channel URL and add it to external links (with dedup).
	if canonicalURL, ok := viewModel["canonicalChannelUrl"].(string); ok && canonicalURL != "" {
		fullURL := ensureHTTPS(canonicalURL)
		// Only add if not already present in external links.
		found := false
		for _, existing := range info.ExternalLinks {
			if existing == fullURL {
				found = true
				break
			}
		}
		if !found {
			// Prepend YouTube link as the first link.
			info.ExternalLinks = append([]string{fullURL}, info.ExternalLinks...)
		}
	}

	log.Printf("INFO: [youtube] Enriched author %s: followers=%d, videos=%d, totalPlay=%d, joinDate=%v, region=%s, links=%d",
		info.ChannelID, info.Followers, info.VideoCount, info.TotalPlayCount, info.JoinDate, info.Region, len(info.ExternalLinks))
}

// parseHumanCount parses human-readable count strings like "43.3M subscribers" or "121 videos" to int64.
var humanCountRegex = regexp.MustCompile(`([\d,.]+)\s*([KkMmBb]?)`)

func parseHumanCount(s string) int64 {
	matches := humanCountRegex.FindStringSubmatch(s)
	if len(matches) < 2 {
		return 0
	}

	numStr := strings.ReplaceAll(matches[1], ",", "")
	suffix := strings.ToUpper(matches[2])

	// Parse the numeric part.
	var value float64
	fmt.Sscanf(numStr, "%f", &value)

	switch suffix {
	case "K":
		return int64(value * 1000)
	case "M":
		return int64(value * 1_000_000)
	case "B":
		return int64(value * 1_000_000_000)
	default:
		return int64(value)
	}
}

// joinDateRegex matches "Joined Mon DD, YYYY" format.
var joinDateRegex = regexp.MustCompile(`(?i)joined\s+(\w+\s+\d{1,2},\s+\d{4})`)

// parseJoinDate parses a join date string like "Joined Sep 19, 2006" to time.Time.
func parseJoinDate(s string) time.Time {
	matches := joinDateRegex.FindStringSubmatch(s)
	if len(matches) < 2 {
		return time.Time{}
	}

	t, err := time.Parse("Jan 2, 2006", matches[1])
	if err != nil {
		log.Printf("WARN: [youtube] failed to parse join date %q: %v", s, err)
		return time.Time{}
	}
	return t
}

// ensureHTTPS ensures a URL has the "https://" scheme prefix.
// If the URL already starts with "http://" or "https://", it is returned as-is.
func ensureHTTPS(url string) string {
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		return url
	}
	return "https://" + url
}
