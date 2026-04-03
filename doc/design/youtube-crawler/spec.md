# Feature: YouTube 爬虫

**作者**: dylanyuanZ  
**日期**: 2026-04-02  
**状态**: Draft

---

## 1. 背景 (Background)

### 1.1 问题描述

系统目前仅支持 Bilibili 平台的爬虫，无法抓取 YouTube 平台的视频搜索结果和作者信息。需要新增 YouTube 爬虫实现，使系统具备跨平台数据抓取能力。

### 1.2 现状分析

**现有架构**：系统采用三阶段流水线架构（Stage 0 → Stage 1 → Stage 2），核心编排逻辑在 `src/crawler.go` 中：

- **Stage 0（搜索）**：`RunStage0()` 调用 `SearchCrawler.SearchPage()` 接口，按页抓取搜索结果，输出 video CSV + author mids JSON
- **Stage 1（作者基本信息）**：`RunStage1()` 调用 `AuthorCrawler.FetchAuthorInfo()` 接口，获取作者基本信息，输出 author CSV
- **Stage 2（作者详情+视频遍历）**：`RunStage2()` 调用 `AuthorCrawler.FetchAuthorInfo()` + `FetchAllAuthorVideos()` 接口，获取完整作者数据

**平台接口**：
- `SearchCrawler` 接口：`SearchPage(ctx, keyword, page) → ([]Video, PageInfo, error)`
- `AuthorCrawler` 接口：`FetchAuthorInfo(ctx, mid) → (*AuthorInfo, error)` + `FetchAllAuthorVideos(ctx, mid, maxVideos) → ([]VideoDetail, PageInfo, error)`

**Bilibili 实现**：位于 `src/platform/bilibili/`，通过 HTTP API 调用 + rod 浏览器自动化实现

**YouTube 技术调研结论**（来自探针验证报告）：
- YouTube 是 SSR 模式，核心数据在 `window.ytInitialData` 全局变量中
- 搜索页面通过 URL 参数 `sp=` 控制筛选，支持类型/时长/上传日期/排序的组合筛选
- 搜索结果通过滚动加载（非分页），到底部时 `continuationItemRenderer` 消失
- 作者基本信息（姓名、粉丝数、视频数等）可从 SSR 直接获取
- 注册时间、总播放量、地区、完整外部链接需点击描述区域触发 browse API 获取
- Videos tab 和 Shorts tab 外层结构一致（`richGridRenderer`），但内容渲染器不同
- 无需登录/Cookie

**配置现状**（需重构）：
- 当前配置（`conf/config.yaml`）全部为全局参数，`concurrency`、`request_interval`、`max_consecutive_failures` 等未按阶段区分
- `cookie` 等参数未按平台区分，目前只有 Bilibili 的 cookie
- 新增 YouTube 平台时需要支持平台级筛选参数（类型/时长/上传日期/排序），现有配置结构无法承载

### 1.3 主要使用场景

与 Bilibili 爬虫使用场景一致：用户通过关键词搜索 YouTube 视频，系统自动抓取搜索结果（Stage 0）和作者信息（Stage 1），输出 video CSV 和 author CSV 供后续分析使用。

## 2. 目标 (Goals)

1. **实现 YouTube 爬虫**：实现 `SearchCrawler` 和 `AuthorCrawler` 接口，支持 Stage 0（搜索）和 Stage 1（作者基本信息），Stage 2 提供空实现
2. **配置重构**：将现有全局配置重构为分阶段、分平台的结构，同时新增 YouTube 平台的筛选配置项
3. **输出 CSV**：与 Bilibili 一致，输出 video CSV 和 author CSV

### 2.1 非目标 (Non-Goals)

- 不做 Stage 2 的视频遍历和统计计算（YouTube 的 Stage 2 为空实现）
- 不做评论抓取
- 不做 YouTube 登录/Cookie 管理（YouTube 数据无需登录即可获取）

## 3. 需求细化 (Requirements)

### 3.1 功能性需求

#### FR-1: 编排层接口重构

**FR-1.1: Stage 0 — 新增 `SearchRecorder` 接口**

现有 `SearchCrawler.SearchPage(ctx, keyword, page)` 接口将 "search + record" 的编排逻辑（先抓第一页获取 TotalPages → 并发抓剩余页）硬编码在编排层 `RunStage0` 中，这是 Bilibili 特有的分页策略。YouTube 采用滚动加载模式，无法复用此编排逻辑。

**方案**：抽取 `SearchRecorder` 接口，将 "search + record" 的完整流程下沉到平台层：

```go
type SearchRecorder interface {
    SearchAndRecord(ctx context.Context, keyword string, csvWriter CSVRowWriter, progress ProgressTracker) (int, error)
}
```

- `csvWriter` 传入，平台内部实时写 CSV
- `progress` 传入，平台内部自己处理断点续爬
- 配置参数（MaxSearchPage、并发度等）由平台自己从全局 config 读取
- 原有 `SearchCrawler.SearchPage()` 接口废弃（或降级为平台内部方法）

`RunStage0` 瘦身后的职责：
1. 创建/打开 CSV writer
2. 调用 `SearchAndRecord(ctx, keyword, csvWriter, progress)`
3. 读 CSV 做去重 → 输出 `[]AuthorMid`
4. 写中间数据文件 + 更新 progress

各平台实现差异：
| | Bilibili | YouTube |
|---|---------|---------|
| 内部策略 | 先抓第一页获取 TotalPages → Worker Pool 并发抓剩余页 | 滚动加载 → 每批写 CSV → 判断到底 |
| 并发 | 多并发（从 config 读） | 单线程串行滚动 |
| 限制 | MaxSearchPage（从 config 读） | MaxScrollCount（从 config 读） |

**FR-1.2: Stage 1 — `AuthorCrawler` 接口保持不变**

Stage 1 的 `FetchAuthorInfo(ctx, mid)` 模式对 YouTube 也适用（每个作者独立访问一个页面），不需要改动。但编排层 `processOneAuthorBasic` 需要适配通用 CSV writer（见 FR-2）。

**FR-1.3: Stage 2 — YouTube 提供空实现**

YouTube 的 `AuthorCrawler.FetchAllAuthorVideos()` 返回空列表，Stage 2 不执行实际逻辑。

#### FR-2: 平台类型下沉 + CSV Writer 通用化

**FR-2.1: 平台类型下沉**

将 `src/types.go` 中的平台相关结构体移到各自平台包下：

| 结构体 | 现在位置 | 目标位置 | 原因 |
|--------|---------|---------|------|
| `Video` | `src/types.go` | `src/platform/bilibili/types.go` | 字段（如 `Source`）是 Bilibili 特有的，YouTube 的视频字段不同 |
| `AuthorInfo` | `src/types.go` | 各平台包下 | `TotalLikes` 是 Bilibili 特有，YouTube 有 `JoinDate`/`Region` 等 |
| `VideoDetail` | `src/types.go` | `src/platform/bilibili/types.go` | `BvID` 是 Bilibili 特有 |
| `AuthorStats` | `src/types.go` | `src/platform/bilibili/types.go` | Stage 2 统计，仅 Bilibili 使用 |
| `TopVideo` | `src/types.go` | `src/platform/bilibili/types.go` | Stage 2 TOP 视频，仅 Bilibili 使用 |
| `Author` | `src/types.go` | 各平台包下 | 字段组合因平台而异 |

`src/types.go` 只保留跨平台通用的类型：`PageInfo`、`AuthorMid`、`ProgressTracker`、以及新的通用接口。

**FR-2.2: CSV Writer 通用化**

现有 CSV Writer 绑定了具体业务类型（`VideoCSVWriter.WriteRows([]Video)`、`AuthorCSVWriter.WriteRow(Author)`），改为只操作 `[]string`：

```go
// CSVWriter — 通用 CSV writer，只关心 header + []string rows
type CSVWriter struct { ... }
func (cw *CSVWriter) WriteRow(row []string) error { ... }
func (cw *CSVWriter) WriteRows(rows [][]string) error { ... }
```

- `VideoCSVAdapter` 和 `AuthorCSVAdapter` 接口废弃
- `VideoCSVRowWriter` 和 `AuthorCSVRowWriter` 接口合并为一个通用 `CSVRowWriter`：

```go
type CSVRowWriter interface {
    WriteRow(row []string) error
    WriteRows(rows [][]string) error
    FilePath() string
    Close() error
}
```

- 类型转换的职责归平台：平台自己把 `BilibiliVideo → []string`、`YouTubeAuthor → []string`

**FR-2.3: `ReadVideoCSV` 返回 `[]AuthorMid`**

`ReadVideoCSV` 的唯一用途是去重作者，不需要返回完整的 `[]Video`。改为直接返回 `[]AuthorMid`，由平台提供 AuthorID 和 Author Name 所在的列索引。

**FR-2.4: 编排层 `processOneAuthorBasic` / `processOneAuthorFull` 适配**

编排层不再感知平台特有字段。Stage 1 的 `AuthorCrawler.FetchAuthorInfo` 返回的数据由平台自己转为 `[]string` 写入 CSV。具体方案：

- `FetchAuthorInfo` 返回值改为 `[]string`（即 CSV 行），编排层直接调用 `csvWriter.WriteRow(row)` 写入
- 或者 `FetchAuthorInfo` 返回一个通用的 `AuthorInfo` 接口，平台自己实现 `ToCSVRow() []string`

（具体方案在设计章节细化）

#### FR-3: YouTube 爬虫实现

**FR-3.1: Stage 0 — 搜索**

- 按关键词搜索 YouTube 视频，通过 URL 参数 `sp=` 控制筛选
- 滚动加载全部结果，通过 `continuationItemRenderer` 消失判断到底
- 提取字段：视频名、播放量、发布时间、作者名、视频说明、视频时长、视频ID、频道ID
- 输出 video CSV + author mids JSON

**FR-3.2: Stage 1 — 作者信息**

- 访问频道页，从 SSR 提取基础信息（姓名、粉丝数、视频数、handle）
- 点击描述区域触发 browse API，获取注册时间、总播放量、地区、完整外部链接
- 输出 author CSV

#### FR-4: 配置重构

**FR-4.1: 分阶段配置**

将全局参数（`concurrency`、`request_interval`、`max_consecutive_failures` 等）改为分阶段配置，每个 Stage 可以有独立的并发度和请求间隔。

**FR-4.2: 分平台配置**

将平台相关参数（`cookie`、筛选项等）改为分平台配置。

**FR-4.3: YouTube 筛选配置**

新增 YouTube 平台的筛选配置项（具体筛选项后续澄清，预留配置结构）。

### 3.2 非功能性需求

- **NFR-1: 编译兼容**：每个重构步骤完成后，`go build ./...` 必须通过
- **NFR-2: 向后兼容**：Bilibili 爬虫的现有功能不受影响，行为保持一致
- **NFR-3: 断点续爬**：YouTube 爬虫支持断点续爬（通过 Progress 机制）
- **NFR-4: 无需登录**：YouTube 爬虫不需要登录/Cookie 管理

## 4. 设计方案 (Design)

### 4.1 方案概览

本次改动分为两大部分：**架构重构**（FR-1、FR-2、FR-4）和 **YouTube 爬虫实现**（FR-3）。

架构重构的核心思路是 **"编排层通用化，平台层自治"**：
- 编排层（`crawler.go`）只操作 `[]string`（CSV 行）和 `AuthorMid`，不感知任何平台特有类型
- 平台层（`platform/bilibili/`、`platform/youtube/`）各自维护自己的数据类型和 CSV 转换逻辑
- CSV Writer 通用化为只操作 `header + []string rows`

```
┌─────────────────────────────────────────────────────┐
│                    main.go (入口)                     │
│  - 解析配置、创建 browser、组装 StageConfig           │
└──────────────┬──────────────────────┬────────────────┘
               │                      │
    ┌──────────▼──────────┐  ┌───────▼────────────┐
    │   crawler.go (编排)  │  │  config (配置管理)  │
    │  RunStage0/1/2      │  │  分平台、分阶段     │
    │  只操作 []string     │  └────────────────────┘
    │  + AuthorMid        │
    └──────┬──────────────┘
           │ 调用平台接口
    ┌──────▼──────────────────────────────────────┐
    │              平台层 (Platform)                │
    │  ┌─────────────────┐  ┌──────────────────┐  │
    │  │    bilibili/     │  │    youtube/      │  │
    │  │  types.go        │  │  types.go        │  │
    │  │  csv.go          │  │  csv.go          │  │
    │  │  search.go       │  │  search.go       │  │
    │  │  author.go       │  │  author.go       │  │
    │  └─────────────────┘  └──────────────────┘  │
    └──────────────┬──────────────────────────────┘
                   │
    ┌──────────────▼──────────────────┐
    │     export/ (通用 CSV Writer)    │
    │  CSVWriter.WriteRow([]string)   │
    │  CSVWriter.WriteRows([][]string)│
    │  ReadVideoCSV → []AuthorMid     │
    └─────────────────────────────────┘
```

### 4.2 组件设计 (Component Design)

#### 4.2.1 核心类/模块设计

**变更文件清单**：

| 文件 | 操作 | 说明 |
|------|------|------|
| `src/types.go` | 修改 | 删除平台特有类型，保留通用类型 + 新接口 |
| `src/crawler.go` | 修改 | RunStage0 瘦身，RunStage1/2 适配通用 CSV |
| `src/export/export.go` | 修改 | CSV Writer 通用化，ReadVideoCSV 返回 `[]AuthorMid` |
| `src/platform/bilibili/types.go` | 新建 | 承接从 `src/types.go` 移出的 Bilibili 类型 |
| `src/platform/bilibili/csv.go` | 修改 | 适配通用 CSV Writer |
| `src/platform/bilibili/search.go` | 修改 | 实现 `SearchRecorder` 接口 |
| `src/platform/youtube/types.go` | 新建 | YouTube 平台类型定义 |
| `src/platform/youtube/csv.go` | 新建 | YouTube CSV 转换逻辑 |
| `src/platform/youtube/search.go` | 新建 | YouTube `SearchRecorder` 实现 |
| `src/platform/youtube/author.go` | 新建 | YouTube `AuthorCrawler` 实现 |
| `src/config/config.go` | 修改 | 配置结构重构为分阶段、分平台 |
| `conf/config.yaml` | 修改 | 配置文件结构重构 |
| `cmd/crawler/main.go` | 修改 | 适配新接口和新配置结构 |

#### 4.2.2 接口设计

**新增接口**：

```go
// SearchRecorder — 替代原有 SearchCrawler，将 search+record 完整流程下沉到平台
type SearchRecorder interface {
    SearchAndRecord(ctx context.Context, keyword string, csvWriter CSVRowWriter, progress ProgressTracker) (int, error)
}

// CSVRowWriter — 通用 CSV 行写入接口（合并原有 VideoCSVRowWriter + AuthorCSVRowWriter）
type CSVRowWriter interface {
    WriteRow(row []string) error
    WriteRows(rows [][]string) error
    FilePath() string
    Close() error
}
```

**保留接口**（签名调整）：

```go
// AuthorCrawler — 保留，但 FetchAuthorInfo 返回值改为 []string（CSV 行）
type AuthorCrawler interface {
    FetchAuthorInfo(ctx context.Context, mid string) ([]string, error)
    FetchAllAuthorVideos(ctx context.Context, mid string, maxVideos int) ([][]string, error)
}
```

**废弃接口**：
- `SearchCrawler`（被 `SearchRecorder` 替代）
- `VideoCSVAdapter`（平台直接提供 header + toRow）
- `AuthorCSVAdapter`（平台直接提供 header + toRow）
- `VideoCSVRowWriter`（被通用 `CSVRowWriter` 替代）
- `AuthorCSVRowWriter`（被通用 `CSVRowWriter` 替代）

#### 4.2.3 数据模型

**`src/types.go` 保留的通用类型**：

```go
type PageInfo struct { TotalPages, TotalCount int }
type AuthorMid struct { Name, ID string }
type PoolResult[T, R any] struct { Task T; Result R; Err error }
```

**Bilibili 平台类型**（`src/platform/bilibili/types.go`）：

```go
type Video struct {
    Title, Author, AuthorID string
    PlayCount int64
    PubDate time.Time
    Duration int
    Source string
}

type AuthorInfo struct {
    Name string; Followers, TotalLikes, TotalPlayCount int64; VideoCount int
}

type VideoDetail struct {
    Title, BvID string
    PlayCount, CommentCount, LikeCount int64
    Duration int; PubDate time.Time
}

type AuthorStats struct { AvgPlayCount, AvgDuration, AvgCommentCount float64 }
type TopVideo struct { Title, URL string; PlayCount int64 }
type Author struct { Name, ID string; Followers int64; VideoCount int; TotalLikes, TotalPlayCount int64; Stats AuthorStats; TopVideos []TopVideo }
```

**YouTube 平台类型**（`src/platform/youtube/types.go`）：

```go
type Video struct {
    Title, Author, ChannelID, VideoID, Description string
    PlayCount int64
    PubDate time.Time
    Duration int
}

type AuthorInfo struct {
    Name, Handle, ChannelID, Description, Region string
    Followers, TotalPlayCount int64
    VideoCount int
    JoinDate time.Time
    ExternalLinks []string
}
```

#### 4.2.4 并发模型

- **Stage 0**：由平台自己决定并发策略
  - Bilibili：Worker Pool 并发抓取多页
  - YouTube：单线程串行滚动
- **Stage 1**：编排层 Worker Pool 并发处理多个作者（不变）
- **Stage 2**：编排层 Worker Pool + Cooldown + Retry（不变，YouTube 为空实现）

#### 4.2.5 错误处理

- 与现有机制一致：`MaxConsecutiveFailures` 连续失败熔断、`IsRetryableError` 平台级可重试判断
- YouTube 的 `SearchAndRecord` 内部处理滚动失败和重试逻辑

### 4.3 核心逻辑实现

#### 4.3.1 `RunStage0` 瘦身后的流程

```
1. 创建/打开 CSVWriter
2. 调用 searchRecorder.SearchAndRecord(ctx, keyword, csvWriter, progress)
3. 关闭 CSVWriter
4. 调用 ReadVideoCSV(csvPath) → []AuthorMid（平台提供列索引）
5. 写中间数据文件 + 更新 progress
```

#### 4.3.2 `RunStage1` 适配通用 CSV

```
1. 创建/打开 CSVWriter
2. Worker Pool 遍历 mids:
   a. 调用 ac.FetchAuthorInfo(ctx, mid) → []string (CSV 行)
   b. csvWriter.WriteRow(row)
3. 关闭 CSVWriter
```

#### 4.3.3 YouTube SearchAndRecord 实现

```
1. 构建搜索 URL（keyword + sp= 筛选参数，从 config 读取）
2. 导航到搜索页面
3. 提取 ytInitialData 中的视频列表 → 转为 []string → csvWriter.WriteRows()
4. 循环：
   a. 滚动到底部
   b. 等待新内容加载
   c. 提取新增视频 → csvWriter.WriteRows()
   d. 检查 continuationItemRenderer 是否消失 → 消失则结束
   e. 检查是否达到 MaxScrollCount → 达到则结束
5. 返回总视频数
```

### 4.4 方案优劣分析

**优势**：
- 编排层完全通用化，新增平台只需实现 `SearchRecorder` + `AuthorCrawler` 接口
- CSV Writer 不再绑定业务类型，扩展性好
- 平台自治：每个平台维护自己的类型和 CSV 转换逻辑，互不干扰

**劣势/风险**：
- 重构范围较大，涉及多个文件的联动修改
- `FetchAuthorInfo` 返回 `[]string` 丢失了类型安全性（但 CSV 本身就是 `[]string`，这是合理的 trade-off）
- 需要确保 Bilibili 现有功能不受影响（通过编译验证 + 功能测试）

## 5. 备选方案 (Alternatives Considered)

## 6. 业界调研 (Industry Research)

### 6.1 业界方案
### 6.2 对比分析

## 7. 测试计划 (Test Plan)

### 7.1 单元测试
### 7.2 集成测试
### 7.3 性能测试（如适用）

## 8. 可观测性 & 运维 (Observability & Operations)

### 8.1 可观测性
### 8.2 配置参数 (Configuration)

## 9. Changelog

| 日期 | 变更内容 | 作者 |
|------|----------|------|
| 2026-04-02 | 初稿，完成背景章节 | dylanyuanZ |
| 2026-04-02 | 完成目标、需求细化、设计方案章节 | dylanyuanZ |

## 10. 参考资料 (References)

- [YouTube 人工调研文档](../../youtube_research.md)
- [YouTube 探针验证报告 V1](../youtube-probe-verification/feedback_driven_report.md)
- [YouTube 探针验证报告 V2](../youtube-probe-verification/feedback_driven_report_v2.md)
