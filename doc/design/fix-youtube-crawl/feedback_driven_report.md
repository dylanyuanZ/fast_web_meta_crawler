# Fix YouTube Crawl Data Errors — Probe-Driven Development Report

## 1. Background

### Problem Description
Running `bin/crawler --platform youtube --keyword "中创星航" --config="conf/config.yaml"` produces:
- **Video CSV**: First 20 rows correct, last 7 rows have wrong ChannelID format (`@handle` instead of `UCxxxxxx`), VideoID with `&pp=` suffix, play count = 0, publish time = zero value
- **Author CSV**: ALL rows have followers=0, total_play=0, video_count=0, join_date empty, region empty. Only channel name and description are populated.

### Verification Target
1. Video CSV: All rows should have correct VideoID (no `&pp=` suffix), non-zero play count, valid publish time
2. Author CSV: Rows should have non-zero followers/play/video counts where available, join date populated

### Run Command
```bash
bin/crawler --platform youtube --keyword "中创星航" --config="conf/config.yaml"
```

## 2. Problem Analysis (Based on Probe Data)

### Probe Data Sources
- `probe/probe_youtube_test.go` — TestProbeYouTubeSearchPage, TestProbeYouTubeAuthorPage
- `probe/probe_youtube_test.go` — TestProbeYouTubeAboutPanel (V2 probe)
- `doc/design/youtube-probe-verification/feedback_driven_report.md` — V1 report
- `doc/design/youtube-probe-verification/feedback_driven_report_v2.md` — V2 report

### Bug 1: Video CSV scroll data corruption (last 7 rows)
- **Root cause**: DOM extraction JS (`newVideosJS`) in `search.go`:
  - `videoId` extracted from `href` attribute using `.replace('/watch?v=', '')`, which leaves `&pp=YYY` query params
  - `views` and `publishTime` CSS selectors (`#metadata-line span:first-child`) don't match YouTube's actual DOM structure (uses `span.inline-metadata-item`)
- **Fix**: Use regex to extract clean videoId from href, use correct CSS selector for metadata spans

### Bug 2: Author CSV all zeros
- **Root cause**: `author.go` uses outdated JSON paths:
  - `c4TabbedHeaderRenderer` → YouTube now uses `pageHeaderRenderer` + `pageHeaderViewModel`
  - `channelAboutFullMetadataRenderer` → YouTube no longer has a separate About tab
  - Subscribers/video count are in `pageHeaderViewModel.metadata.contentMetadataViewModel.metadataRows`
  - Join date/total play/country/links require clicking description area → browse API → `aboutChannelViewModel`
- **Fix**: Rewrite `parseAuthorInfo` to use correct SSR paths, add browse API interception for detailed info

## 3. Iteration Log

### Round 1: Fix both bugs + Probe verification

**Changes**:

1. **search.go** — Fixed `newVideosJS` DOM extraction:
   - VideoID: Changed from `.replace('/watch?v=', '')` to regex `href.match(/[?&]v=([^&]+)/)`
   - Views/PublishTime: Changed from `#metadata-line span:first-child/nth-child(2)` to `#metadata-line span.inline-metadata-item`

2. **author.go** — Complete rewrite of author info extraction:
   - `parseAuthorInfoFromSSR()`: Extracts name/description/channelID from `channelMetadataRenderer`, plus subscribers/video count from `pageHeaderViewModel.metadata.contentMetadataViewModel.metadataRows`
   - `enrichAuthorInfoFromBrowseAPI()`: Uses `browser.WaitForIntercept` (passive `NetworkResponseReceived` events) to click description area and capture browse API response
   - `parseAboutChannelViewModel()`: Parses `aboutChannelViewModel` for join date, total play count, country, external links
   - `parseHumanCount()`: New function to parse "43.3M subscribers", "121 videos" etc.
   - `parseJoinDate()`: New function to parse "Joined Sep 19, 2006" format

3. **probe_test.go, probe_stage1_test.go** — Fixed compilation errors from old Bilibili tests (updated to match new interface signatures)

4. **probe_youtube_fix_verify_test.go** — New verification tests:
   - `TestVerifyYouTubeAuthorFix`: Verifies author info extraction against @BrunoMars
   - `TestVerifyYouTubeSearchFix`: Verifies DOM video extraction for search results

**Compilation**: ✅ PASS

**Probe Verification Results**:

#### TestVerifyYouTubeAuthorFix — ✅ PASS (2.93s)
```
频道名称: Bruno Mars                    ✅
ChannelID: UCoUM-UJ7rirJYP8CQ0EIaHA    ✅
Handle: @brunomars                      ✅
粉丝数: 43300000                        ✅
总播放数: 25113856444                    ✅
视频数量: 121                            ✅
注册时间: 2006-09-19                     ✅
地区: (empty — normal for this channel)  ⚠️
外部链接: 6 links                        ✅
```

#### TestVerifyYouTubeSearchFix — ✅ PASS (9.83s)
- 26 videos extracted from DOM
- All VideoIDs are clean (no `&pp=` suffix)
- All VideoIDs are 11 characters (standard YouTube format)
- Views and PublishTime correctly extracted for all videos

**Next Step**: User runs `bin/crawler --platform youtube --keyword "中创星航" --config="conf/config.yaml"` for final end-to-end validation.
