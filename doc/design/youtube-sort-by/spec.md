# YouTube 排序配置

> Status: Quick Draft

## 1. 背景

当前 YouTube 配置中有一个 `sort_by` 字段，但未被任何业务逻辑消费。用户希望为 YouTube 的两个排序场景（搜索页面、作者视频页面）分别配置独立的排序方式。

## 2. 目标

- 将单一的 `sort_by` 拆分为两个独立配置：`search_page_sort_by` 和 `author_page_sort_by`
- 搜索页面排序在 Stage 0 的搜索 URL 中生效（通过 `sp=` 参数）
- 作者视频页面排序在 Stage 1/2 的频道视频页面中生效（通过 URL `sort=` 参数）

## 3. 需求

### 3.1 搜索页面排序 (`search_page_sort_by`)

- 可选值：`"relevance"`（默认）、`"popularity"`
- 作用于 `buildSPParam()` 函数，影响搜索 URL 的 `sp=` 参数
- YouTube 搜索排序的 sp 值（来源：探针报告 `searchFilterOptionsDialogRenderer` 实测数据）：
  - relevance: 无需额外 sp（默认）
  - popularity: `CAM%3D`
- 排序需要与现有 filter（type/duration/upload）组合使用

### 3.2 作者视频页面排序 (`author_page_sort_by`)

- 可选值：`"newest"`（默认）、`"popular"`、`"oldest"`
- 作用于频道视频页面的 URL 参数
- YouTube 频道视频排序参数：
  - newest: `sort=dd`（默认）
  - popular: `sort=p`
  - oldest: `sort=da`
- 注意：当前 `FetchAllAuthorVideos` 是 no-op，排序配置先预留，待频道视频抓取功能实现后生效

### 3.3 配置打印

- 在启动日志中打印两个排序配置的值

## 4. 设计

### 4.1 配置结构体变更

```go
type YouTubeConfig struct {
    FilterType         string `yaml:"filter_type"`
    FilterDuration     string `yaml:"filter_duration"`
    FilterUpload       string `yaml:"filter_upload"`
    SearchPageSortBy   string `yaml:"search_page_sort_by"`    // "relevance", "popularity"
    AuthorPageSortBy   string `yaml:"author_page_sort_by"`    // "newest", "popular", "oldest"
    Concurrency        int    `yaml:"concurrency"`
    RequestInterval    time.Duration `yaml:"request_interval"`
}
```

### 4.2 搜索排序实现

在 `buildSPParam()` 中，排序需要与 filter 组合。当同时有 filter 和 sort 时，需要将两者编码到同一个 sp 参数中。

简化方案：当配置了非 relevance 的排序时，优先使用排序对应的 sp 值（排序优先于 filter）。如果同时配置了 filter_upload，则使用包含排序+filter 的组合 sp 值。

### 4.3 作者视频页面排序

在 `FetchAllAuthorVideos` 中预留排序参数的读取逻辑，当前仍为 no-op 但日志中打印配置的排序方式。
