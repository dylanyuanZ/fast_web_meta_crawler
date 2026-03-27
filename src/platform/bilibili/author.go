package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/httpclient"
)

const (
	userInfoAPI   = "https://api.bilibili.com/x/space/acc/info"
	userStatAPI   = "https://api.bilibili.com/x/relation/stat"
	videoListAPI  = "https://api.bilibili.com/x/space/wbi/arc/search"
	videoPageSize = 50 // Bilibili default page size for video list

	// maxAPIRetries is the max number of retries for retryable API business errors.
	maxAPIRetries = 5
	// apiRetryDelay is the base delay between retries for API business errors.
	apiRetryDelay = 3 * time.Second
)

// BiliAuthorCrawler implements src.AuthorCrawler for Bilibili.
type BiliAuthorCrawler struct {
	client *httpclient.Client
}

// Compile-time interface check.
var _ src.AuthorCrawler = (*BiliAuthorCrawler)(nil)

// NewAuthorCrawler creates a new BiliAuthorCrawler.
// Also initializes the global wbi signer with the shared HTTP client.
func NewAuthorCrawler(client *httpclient.Client) *BiliAuthorCrawler {
	// Initialize the global wbi signer if not already done.
	if globalWbiSigner == nil {
		globalWbiSigner = &wbiSigner{client: client}
	}
	return &BiliAuthorCrawler{client: client}
}

// FetchAuthorInfo fetches basic author info (name, followers, region).
// Combines data from user info API and user stat API.
// Automatically retries on transient API errors (-799, -352).
func (c *BiliAuthorCrawler) FetchAuthorInfo(ctx context.Context, mid string) (*src.AuthorInfo, error) {
	// Fetch user profile info with retry.
	var infoResp UserInfoResp
	err := retryOnAPIError(ctx, fmt.Sprintf("user info mid=%s", mid), func() error {
		infoURL := fmt.Sprintf("%s?mid=%s", userInfoAPI, mid)
		infoBody, err := c.client.Get(ctx, infoURL)
		if err != nil {
			return fmt.Errorf("fetch user info mid=%s: %w", mid, err)
		}
		if err := json.Unmarshal(infoBody, &infoResp); err != nil {
			return fmt.Errorf("parse user info: %w", err)
		}
		if infoResp.Code != 0 {
			return &apiError{Code: infoResp.Code, Message: infoResp.Message, Context: fmt.Sprintf("user info mid=%s", mid)}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Fetch user follower stats with retry.
	var statResp UserStatResp
	err = retryOnAPIError(ctx, fmt.Sprintf("user stat mid=%s", mid), func() error {
		statURL := fmt.Sprintf("%s?vmid=%s", userStatAPI, mid)
		statBody, err := c.client.Get(ctx, statURL)
		if err != nil {
			return fmt.Errorf("fetch user stat mid=%s: %w", mid, err)
		}
		if err := json.Unmarshal(statBody, &statResp); err != nil {
			return fmt.Errorf("parse user stat: %w", err)
		}
		if statResp.Code != 0 {
			return &apiError{Code: statResp.Code, Message: statResp.Message, Context: fmt.Sprintf("user stat mid=%s", mid)}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &src.AuthorInfo{
		Name:      infoResp.Data.Name,
		Followers: statResp.Data.Follower,
		Region:    "", // Bilibili user info API does not reliably expose region
	}, nil
}

// FetchAuthorVideos fetches a single page of the author's video list.
// Automatically retries on transient API errors (-799, -352).
func (c *BiliAuthorCrawler) FetchAuthorVideos(ctx context.Context, mid string, page int) ([]src.VideoDetail, src.PageInfo, error) {
	var resp VideoListResp
	err := retryOnAPIError(ctx, fmt.Sprintf("video list mid=%s page=%d", mid, page), func() error {
		rawURL := fmt.Sprintf("%s?mid=%s&pn=%d&ps=%d&order=pubdate", videoListAPI, mid, page, videoPageSize)
		// Sign the URL with wbi parameters (w_rid, wts) required by Bilibili.
		signedURL, signErr := globalWbiSigner.signURL(rawURL)
		if signErr != nil {
			return fmt.Errorf("wbi sign: %w", signErr)
		}
		body, err := c.client.Get(ctx, signedURL)
		if err != nil {
			return fmt.Errorf("fetch videos mid=%s page=%d: %w", mid, page, err)
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("parse video list: %w", err)
		}
		if resp.Code != 0 {
			return &apiError{Code: resp.Code, Message: resp.Message, Context: fmt.Sprintf("video list mid=%s page=%d", mid, page)}
		}
		return nil
	})
	if err != nil {
		return nil, src.PageInfo{}, err
	}

	videos := make([]src.VideoDetail, 0, len(resp.Data.List.Vlist))
	for _, item := range resp.Data.List.Vlist {
		videos = append(videos, src.VideoDetail{
			Title:        item.Title,
			BvID:         item.BvID,
			PlayCount:    item.Play,
			CommentCount: item.Comment,
			LikeCount:    0, // Bilibili video list API does not return like count directly
			Duration:     parseVideoDuration(item.Length),
			PubDate:      time.Unix(item.Created, 0),
		})
	}

	totalCount := resp.Data.Page.Count
	totalPages := 1
	if videoPageSize > 0 {
		totalPages = int(math.Ceil(float64(totalCount) / float64(videoPageSize)))
	}

	pageInfo := src.PageInfo{
		TotalPages: totalPages,
		TotalCount: totalCount,
	}

	return videos, pageInfo, nil
}

// VideoPageSize returns the page size used for video list requests.
// Exposed for the orchestration layer to calculate max pages.
func VideoPageSize() int {
	return videoPageSize
}

// parseVideoDuration converts a video duration string like "12:34" to seconds.
// Reuses the same logic as search duration parsing.
func parseVideoDuration(s string) int {
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

	return int(math.Abs(float64(total)))
}

// retryOnAPIError retries the given function if it returns a retryable apiError.
// Uses exponential backoff starting from apiRetryDelay.
func retryOnAPIError(ctx context.Context, label string, fn func() error) error {
	delay := apiRetryDelay
	for attempt := 0; attempt <= maxAPIRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		// Check if it's a retryable API error.
		if ae, ok := err.(*apiError); ok && ae.IsRetryable() {
			if attempt < maxAPIRetries {
				// On -352 (risk control), invalidate wbi keys so they are re-fetched.
				// This handles the case where keys have expired mid-session.
				if ae.Code == -352 && globalWbiSigner != nil {
					globalWbiSigner.invalidateKeys()
				}
				log.Printf("WARN: API retry %d/%d for %s: %v, waiting %v", attempt+1, maxAPIRetries, label, err, delay)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
				delay *= 2 // exponential backoff
				continue
			}
		}

		// Non-retryable error or retries exhausted.
		return err
	}
	return fmt.Errorf("unexpected: retry loop exited for %s", label)
}
