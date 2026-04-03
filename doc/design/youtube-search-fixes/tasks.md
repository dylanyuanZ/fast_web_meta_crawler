# 实施任务清单

> 用户反馈的 4 个优化需求
> 任务总数: 4
> 核心原则: 4 个独立需求互不依赖，按影响面从小到大排列

## 依赖关系总览

```
Task 1 (max_search_videos 截断修复)     — 独立
Task 2 (打印平台配置参数)               — 独立
Task 3 (外部链接智能去重)               — 独立
Task 4 (外部链接做成超链接)             — 依赖 Task 3（同一文件同一函数）
```

## 任务列表

### 任务 1: [x] 修复 max_search_videos 对 YouTube 搜索结果的截断
- 文件: `src/platform/youtube/search.go`（修改）
- 依赖: 无
- 说明: 当前滚动循环中，退出条件 `totalVideos >= cfg.MaxSearchVideos` 只在循环开头检查，但新视频在循环末尾全量写入 CSV。如果某次滚动后 DOM 中新增大量视频（如 Shorts 搜索），会导致实际写入远超限制。修复方案：在构建 newVideos 时，截断到 `MaxSearchVideos - totalVideos` 的剩余配额；同时对初始结果也做截断。
- 验收标准: `go build ./...` 通过 + 新视频写入前会截断到剩余配额
- 子任务:
  - [x] 1.1: 初始结果写入前，如果 `len(videos) > cfg.MaxSearchVideos`，截断 videos
  - [x] 1.2: 滚动循环中，构建 newVideos 时，计算 `remaining := cfg.MaxSearchVideos - totalVideos`，如果 `newCount > remaining` 则截断

### 任务 2: [x] 启动时打印平台相关配置参数
- 文件: `cmd/crawler/main.go`（修改）
- 依赖: 无
- 说明: 当前只打印 platform/keyword/stage/concurrency/output，缺少 max_search_videos、max_video_per_author、request_interval 等全局参数，以及平台特有参数（YouTube 的 filter_type/filter_upload 等，Bilibili 的 cookie 状态等）。按用户要求，配置了什么平台就打印什么平台的参数。
- 验收标准: `go build ./...` 通过 + 日志中能看到全局搜索配置和当前平台的特有参数
- 子任务:
  - [x] 2.1: 在现有配置日志后，追加打印全局搜索参数（max_search_videos, max_video_per_author, request_interval）
  - [x] 2.2: 根据 platform 值，打印对应平台的特有参数（YouTube: filter_type, filter_duration, filter_upload; Bilibili: cookie 是否配置）

### 任务 3: [x] 外部链接智能去重（忽略协议/www 差异）
- 文件: `src/platform/youtube/author.go`（修改）
- 依赖: 无
- 说明: 当前外部链接去重只做精确匹配。用户要求：如果两个链接只是相差 http/https 协议前缀或 www. 子域名，应视为同一链接，仅保留其中 1 个。例如 `http://www.youtube.com/@WMTINTL` 和 `https://youtube.com/@WMTINTL` 应去重。
- 验收标准: `go build ./...` 通过 + 相同域名不同协议/www 的链接只保留 1 个
- 子任务:
  - [x] 3.1: 新增 `normalizeURL(url string) string` 函数，去除 http/https 协议前缀和 www. 子域名，返回标准化的 URL 用于比较
  - [x] 3.2: 修改 `parseAboutChannelViewModel` 中外部链接收集逻辑，使用 normalizeURL 进行去重
  - [x] 3.3: 修改 canonicalChannelUrl 去重逻辑，同样使用 normalizeURL 比较

### 任务 4: [x] 外部链接做成可点击的超链接
- 文件: `src/platform/youtube/csv.go`（修改）
- 依赖: Task 3（同一数据流，但文件不同，可独立实现）
- 说明: 当前外部链接以纯文本形式写入 CSV。用户希望在 Excel/Google Sheets 中能直接点击。方案：使用 Excel 的 HYPERLINK 公式格式 `=HYPERLINK("url","url")`，这样在 Excel 中打开 CSV 时链接可直接点击。
- 验收标准: `go build ./...` 通过 + CSV 中外部链接列的每个链接都是 `=HYPERLINK("url","url")` 格式
- 子任务:
  - [x] 4.1: 在 `AuthorInfoToRow` 中，将 ExternalLinks 的每个链接包装为 `=HYPERLINK("url","url")` 格式
