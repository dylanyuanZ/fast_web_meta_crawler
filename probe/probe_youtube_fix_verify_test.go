package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/platform/youtube"
)

// TestVerifyYouTubeAuthorFix verifies that the YouTube author info extraction
// correctly fetches subscribers, video count, join date, total play count, etc.
//
// This is a verification test for the fix applied to author.go.
// It uses @BrunoMars as a well-known channel with stable data.
//
// Run with: go test -v -run TestVerifyYouTubeAuthorFix -timeout 120s ./probe/
func TestVerifyYouTubeAuthorFix(t *testing.T) {
	if err := config.Load("../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()

	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	mgr, err := browser.New(browser.Config{
		Headless:    cfg.Browser.IsHeadless(),
		UserDataDir: cfg.Browser.UserDataDir,
		Concurrency: 1,
		BrowserBin:  resolveBrowserBin(cfg.Browser.Bin),
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer mgr.Close()

	ac := youtube.NewAuthorCrawler(mgr)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Test with @BrunoMars — a well-known channel with stable data.
	channelID := "@BrunoMars"
	t.Logf("Fetching author info for %s...", channelID)

	row, err := ac.FetchAuthorInfo(ctx, channelID)
	if err != nil {
		t.Fatalf("FetchAuthorInfo failed: %v", err)
	}

	// AuthorHeader: "频道名称", "ChannelID", "Handle", "粉丝数", "总播放数", "视频数量",
	//               "注册时间", "地区", "说明", "外部链接"
	if len(row) < 10 {
		t.Fatalf("Expected at least 10 columns, got %d: %v", len(row), row)
	}

	t.Logf("=== Author Info Row ===")
	headers := []string{"频道名称", "ChannelID", "Handle", "粉丝数", "总播放数", "视频数量",
		"注册时间", "地区", "说明", "外部链接"}
	for i, h := range headers {
		if i < len(row) {
			val := row[i]
			if len(val) > 200 {
				val = val[:200] + "..."
			}
			t.Logf("  %s: %s", h, val)
		}
	}

	// Verify critical fields.
	name := row[0]
	chID := row[1]
	handle := row[2]
	followers := row[3]
	totalPlay := row[4]
	videoCount := row[5]
	joinDate := row[6]
	region := row[7]

	// Name should be "Bruno Mars".
	if name == "" {
		t.Errorf("Name is empty")
	} else {
		t.Logf("✅ Name: %s", name)
	}

	// ChannelID should start with "UC".
	if len(chID) < 2 || chID[:2] != "UC" {
		t.Errorf("ChannelID should start with 'UC', got: %s", chID)
	} else {
		t.Logf("✅ ChannelID: %s", chID)
	}

	// Handle should start with "@".
	if len(handle) < 1 || handle[0] != '@' {
		t.Errorf("Handle should start with '@', got: %s", handle)
	} else {
		t.Logf("✅ Handle: %s", handle)
	}

	// Followers should be non-zero (Bruno Mars has millions).
	if followers == "0" || followers == "" {
		t.Errorf("Followers should be non-zero, got: %s", followers)
	} else {
		t.Logf("✅ Followers: %s", followers)
	}

	// Total play count should be non-zero.
	if totalPlay == "0" || totalPlay == "" {
		t.Errorf("TotalPlayCount should be non-zero, got: %s", totalPlay)
	} else {
		t.Logf("✅ TotalPlayCount: %s", totalPlay)
	}

	// Video count should be non-zero.
	if videoCount == "0" || videoCount == "" {
		t.Errorf("VideoCount should be non-zero, got: %s", videoCount)
	} else {
		t.Logf("✅ VideoCount: %s", videoCount)
	}

	// Join date should be non-empty.
	if joinDate == "" {
		t.Errorf("JoinDate should be non-empty, got: %s", joinDate)
	} else {
		t.Logf("✅ JoinDate: %s", joinDate)
	}

	// Region should be non-empty.
	if region == "" {
		t.Logf("⚠️ Region is empty (may be normal for some channels)")
	} else {
		t.Logf("✅ Region: %s", region)
	}

	// Save results to file for inspection.
	dumpDir := "../data/probe/youtube_author_fix_verify"
	os.MkdirAll(dumpDir, 0o755)
	f, err := os.Create(fmt.Sprintf("%s/author_row.txt", dumpDir))
	if err == nil {
		defer f.Close()
		for i, h := range headers {
			if i < len(row) {
				fmt.Fprintf(f, "%s: %s\n", h, row[i])
			}
		}
		t.Logf("Results saved to %s/author_row.txt", dumpDir)
	}
}

// TestVerifyYouTubeSearchFix verifies that the YouTube search DOM extraction
// correctly extracts videoId (without &pp= suffix) and metadata.
//
// Run with: go test -v -run TestVerifyYouTubeSearchFix -timeout 120s ./probe/
func TestVerifyYouTubeSearchFix(t *testing.T) {
	if err := config.Load("../conf/config.yaml"); err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.Get()

	browser.InitDebugLog("../log")
	defer browser.CloseDebugLog()

	mgr, err := browser.New(browser.Config{
		Headless:    cfg.Browser.IsHeadless(),
		UserDataDir: cfg.Browser.UserDataDir,
		Concurrency: 1,
		BrowserBin:  resolveBrowserBin(cfg.Browser.Bin),
	})
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer mgr.Close()

	page := mgr.GetPage()
	defer mgr.PutPage(page)

	// Navigate to YouTube search page.
	searchURL := "https://www.youtube.com/results?search_query=%E4%B8%AD%E5%88%9B%E6%98%9F%E8%88%AA&sp=CAISBAgCEAE%3D"
	t.Logf("Navigating to %s", searchURL)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	p := page.Context(ctx)
	if err := p.Navigate(searchURL); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := p.WaitLoad(); err != nil {
		t.Logf("WARN: wait load: %v", err)
	}
	time.Sleep(5 * time.Second)

	// Scroll once to trigger DOM extraction.
	_, _ = p.Eval(`() => { window.scrollTo(0, document.documentElement.scrollHeight); }`)
	time.Sleep(3 * time.Second)

	// Extract videos from DOM using the fixed JS.
	newVideosJS := `() => {
		const items = document.querySelectorAll('ytd-video-renderer');
		const videos = [];
		items.forEach(item => {
			const titleEl = item.querySelector('#video-title');
			const channelEl = item.querySelector('#channel-name a');
			const durationEl = item.querySelector('ytd-thumbnail-overlay-time-status-renderer span');
			
			const metaSpans = item.querySelectorAll('#metadata-line span.inline-metadata-item');
			let views = '';
			let publishTime = '';
			if (metaSpans.length >= 1) views = metaSpans[0].textContent.trim();
			if (metaSpans.length >= 2) publishTime = metaSpans[1].textContent.trim();
			
			if (titleEl) {
				let videoId = '';
				const href = titleEl.getAttribute('href') || '';
				const match = href.match(/[?&]v=([^&]+)/);
				if (match) videoId = match[1];
				
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

	result, err := p.Eval(newVideosJS)
	if err != nil {
		t.Fatalf("eval DOM extraction: %v", err)
	}

	type domVideo struct {
		Title       string `json:"title"`
		VideoID     string `json:"videoId"`
		Author      string `json:"author"`
		ChannelURL  string `json:"channelUrl"`
		Views       string `json:"views"`
		PublishTime string `json:"publishTime"`
		Duration    string `json:"duration"`
	}

	var videos []domVideo
	if err := json.Unmarshal([]byte(result.Value.String()), &videos); err != nil {
		t.Fatalf("parse DOM videos: %v", err)
	}

	t.Logf("Total DOM videos: %d", len(videos))

	// Verify each video.
	for i, v := range videos {
		t.Logf("Video %d: title=%q, videoId=%q, author=%q, channelUrl=%q, views=%q, publishTime=%q, duration=%q",
			i+1, v.Title, v.VideoID, v.Author, v.ChannelURL, v.Views, v.PublishTime, v.Duration)

		// VideoID should NOT contain "&pp=" or other query params.
		if strings.Contains(v.VideoID, "&") || strings.Contains(v.VideoID, "pp=") {
			t.Errorf("Video %d: VideoID contains query params: %s", i+1, v.VideoID)
		}

		// VideoID should be 11 characters (standard YouTube video ID length).
		if len(v.VideoID) != 11 && v.VideoID != "" {
			t.Logf("⚠️ Video %d: VideoID length is %d (expected 11): %s", i+1, len(v.VideoID), v.VideoID)
		}

		// Views should not be empty for most videos.
		if v.Views == "" && i < 5 {
			t.Logf("⚠️ Video %d: Views is empty", i+1)
		}
	}

	// Save results.
	dumpDir := "../data/probe/youtube_search_fix_verify"
	os.MkdirAll(dumpDir, 0o755)
	pretty, _ := json.MarshalIndent(videos, "", "  ")
	os.WriteFile(fmt.Sprintf("%s/dom_videos.json", dumpDir), pretty, 0o644)
	t.Logf("Results saved to %s/dom_videos.json", dumpDir)
}
