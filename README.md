# Fast Web Meta Crawler

A command-line tool for crawling video platform metadata, designed for influencer-brand marketing analysis. It searches videos by keyword, collects author profiles, and exports structured data as CSV files.

Currently supported platforms: **Bilibili** (YouTube, Instagram, etc. planned).

## Features

- **Two-stage crawling**: Stage 0 searches videos by keyword; Stage 1 collects detailed author profiles and video statistics.
- **Concurrent execution**: Configurable worker pool for parallel crawling.
- **Resumable progress**: Automatic checkpoint saving; resume interrupted tasks from where they left off.
- **Auto retry & backoff**: Built-in HTTP retry with exponential backoff and circuit breaker.
- **Language detection**: Automatically detects the primary language of author content.
- **CSV export**: Outputs video and author data as CSV files for easy analysis.

## Project Structure

```
├── cmd/crawler/       # Entry point (main.go)
├── src/               # Core source code
│   ├── platform/      #   Platform-specific crawlers (e.g. bilibili/)
│   ├── config/        #   Configuration loading
│   ├── export/        #   CSV export
│   ├── httpclient/    #   HTTP client with retry
│   ├── pool/          #   Worker pool
│   ├── progress/      #   Checkpoint / resume
│   └── stats/         #   Statistics & language detection
├── conf/              # Configuration files (config.yaml)
├── doc/               # Design documents
└── go.mod
```

## Prerequisites

- **Go** 1.21 or later

## Build

```bash
# Clone the repository
git clone https://github.com/dylanyuanZ/fast_web_meta_crawler.git
cd fast_web_meta_crawler

# Download dependencies
go mod tidy

# Build the binary
go build -o build/crawler ./cmd/crawler
```

## Usage

```bash
./build/crawler --platform <platform> --keyword <keyword> [--stage 0|1|all] [--config path]
```

### Parameters

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--platform` | Yes | — | Target platform (e.g. `bilibili`) |
| `--keyword` | Yes | — | Search keyword |
| `--stage` | No | `all` | Stage to run: `0` (search only), `1` (author details only), or `all` |
| `--config` | No | `conf/config.yaml` | Path to configuration file |

### Examples

```bash
# Run full pipeline: search + author details
./build/crawler --platform bilibili --keyword "美妆测评"

# Run only Stage 0 (video search)
./build/crawler --platform bilibili --keyword "科技数码" --stage 0

# Run only Stage 1 (author details) using previously saved intermediate data
./build/crawler --platform bilibili --keyword "科技数码" --stage 1

# Use a custom config file
./build/crawler --platform bilibili --keyword "游戏" --config my_config.yaml
```

### Output

CSV files are saved to the directory specified by `output_dir` in the config (default: `data/`):

- `bilibili_<keyword>_<date>_<time>_video.csv` — Video search results
- `bilibili_<keyword>_<date>_<time>_author.csv` — Author profile details

## Configuration

Edit `conf/config.yaml` to customize behavior:

```yaml
# Search
max_search_page: 50          # Max pages to search (1-50)
max_video_per_author: 1000   # Max videos to collect per author

# Concurrency
concurrency: 4               # Worker pool size
request_interval: 500ms      # Delay between requests per worker (anti-crawl)

# HTTP client
http:
  timeout: 10s
  max_retries: 3
  initial_delay: 1s
  max_delay: 10s
  backoff_factor: 2.0

# Circuit breaker
max_consecutive_failures: 5  # Consecutive failures before aborting

# Output
output_dir: "data/"         # CSV output directory

# Cookie (optional, improves success rate for Stage 1)
# cookie: "buvid3=xxx; SESSDATA=xxx"
```

### Cookie Configuration (Optional)

The crawler works without a cookie — it automatically obtains initial cookies (e.g. `buvid3`) by visiting bilibili.com on startup. However, providing a logged-in cookie significantly improves the success rate for Stage 1 (author details), as authenticated requests have higher rate-limit thresholds.

**How to get your Bilibili cookie from Chrome:**

1. Open Chrome and go to [bilibili.com](https://www.bilibili.com). Log in to your account.
2. Press `F12` (or `Ctrl+Shift+I`) to open Developer Tools.
3. Go to the **Network** tab.
4. Refresh the page (`F5`), then click on any request to `bilibili.com` in the list.
5. In the **Headers** panel, find the `Cookie` field under **Request Headers**.
6. Copy the entire cookie string (it looks like `buvid3=xxx; SESSDATA=xxx; bili_jct=xxx; ...`).
7. Paste it into `conf/config.yaml`:

```yaml
cookie: "buvid3=xxx; SESSDATA=xxx; bili_jct=xxx; ..."
```

> **Note:** Cookies expire over time. If you notice increased failures, refresh your cookie.

### Tuning Tips

| Scenario | Recommended Config |
|----------|-------------------|
| Default (balanced) | `concurrency: 4`, `request_interval: 500ms` |
| Aggressive (faster, higher risk) | `concurrency: 8`, `request_interval: 200ms` |
| Conservative (slower, more stable) | `concurrency: 2`, `request_interval: 1s` |
| With proxy IP pool | `concurrency: 8`, `request_interval: 200ms` |

## License

MIT