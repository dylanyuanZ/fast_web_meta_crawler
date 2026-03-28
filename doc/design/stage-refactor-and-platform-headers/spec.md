# Feature: Stage 拆分 + AuthorInfo 扩展 + 平台差异化 Header

**作者**: AI  
**日期**: 2026-03-28  
**状态**: Draft

---

## 1. 背景 (Background)

### 1.1 问题描述

当前系统的 Stage 1 将"获取博主基本信息"和"遍历博主视频列表"耦合在一起。如果用户只需要博主的基本数据（粉丝数、获赞数、总播放量等），仍然必须遍历全部视频列表，造成不必要的时间和资源消耗。

同时存在以下问题：
1. **AuthorInfo 字段不完整**：B 站 `acc/info` 和 `relation/stat` API 返回了总视频数、获赞数、总播放数等字段，但当前代码只解析了 `name`、`sign`、`follower`，大量有价值数据被丢弃
2. **平均值计算方式不合理**：平均播放量、平均点赞量当前通过遍历视频列表计算，但可以直接从 API 返回的汇总数据（总播放数/总视频数）得出，更准确且无需遍历
3. **CSV Header 平台无关**：不同平台能抓取到的数据字段不同（如 B 站有"获赞数"，其他平台可能有"带货数据"），但当前 header 和数据模型是统一的，无法体现平台差异

### 1.2 现状分析

**代码结构**：
- `src/types.go`：平台无关的通用数据模型（`Video`、`AuthorInfo`、`AuthorStats`、`Author`、`VideoDetail`、`TopVideo`）
- `src/crawler.go`：编排层，定义 `SearchCrawler`/`AuthorCrawler` 接口，实现 `RunStage0`/`RunStage1`/`processOneAuthorOnce`
- `src/export/export.go`：CSV 导出层，定义 `videoCSVHeader`（7列）和 `authorCSVHeader`（13列），以及增量写入器
- `src/platform/bilibili/types.go`：B 站 API 响应结构体（`UserData` 只解析 name/sign，`UserStatData` 只解析 follower）
- `src/platform/bilibili/author.go`：`FetchAuthorInfo`（拦截 acc/info + relation/stat）和 `FetchAllAuthorVideos`（翻页采集视频列表）
- `src/stats/stats.go`：`CalcAuthorStats` 从 `[]VideoDetail` 计算平均播放/时长/评论/点赞 + TOP N 视频
- `cmd/crawler/main.go`：CLI 入口，`--stage 0|1|all`

**当前数据流**：
```
Stage 0: SearchPage → []Video → videos.csv + authors.json
Stage 1: FetchAuthorInfo + FetchAllAuthorVideos → CalcAuthorStats → Author → authors.csv
```

**当前 Author CSV 列**（13列）：
博主名字、ID、粉丝数、视频数量、视频平均播放量、视频平均时长、视频平均评论数、视频平均点赞量、视频_TOP1、视频_TOP2、视频_TOP3

**当前 B 站 API 拦截**：
- `/x/space/wbi/acc/info` → 只解析 `name`、`sign`
- `/x/relation/stat` → 只解析 `follower`

**当前 B 站视频列表 API**：
- `/x/space/wbi/arc/search` → 解析 `title`、`bvid`、`play`、`comment`、`length`、`created`
- 注意：视频列表 API **不返回** `like`（点赞数），当前 `LikeCount` 始终为 0

### 1.3 主要使用场景

1. 用户只需要搜索视频列表 → 执行到 Stage 0
2. 用户需要博主基本信息（粉丝数、获赞数、总播放量、平均播放量等）→ 执行到 Stage 1，无需遍历视频列表
3. 用户需要完整博主数据（含平均评论数、平均时长、TOP3 视频）→ 执行到 Stage 2，需要遍历视频列表
4. 未来接入新平台（如抖音）时，不同平台有不同的 CSV 列定义

## 2. 目标 (Goals)

将当前 Stage 1 拆分为 Stage 1（基本博主信息）和 Stage 2（完整博主信息），使用户可以按需选择执行深度；同时扩展 B 站 AuthorInfo 的字段解析，并将 CSV header 和数据模型按平台差异化。

### 2.1 非目标 (Non-Goals)

- **不做**其他平台（如抖音）的接入，本期只处理 B 站
- **不做** Stage 0 的 CSV 输出变更（需求 1 已确认保留现状）
- **不做**视频标题为空的修复（已确认是 B 站搜索 SSR 数据源头问题，保留现状）
- **不做**断点续爬在 Stage 1 和 Stage 2 之间的支持（Stage 1 和 Stage 2 之间不做断点续爬）

## 3. 需求细化 (Requirements)

### 3.1 功能性需求

#### R1: Stage 拆分（编排层改造）

- 将当前 `--stage 0|1|all` 改为 `--stage 0|1|2|all`
- **Stage 0**（搜索）：不变，输出 `videos.csv`
- **Stage 1**（基本博主信息）：只调用 `FetchAuthorInfo`，不遍历视频列表，输出精简版 `authors.csv`
- **Stage 2**（完整博主信息）：先调用 `FetchAuthorInfo`，再遍历视频列表，输出完整版 `authors.csv`
- Stage 1 和 Stage 2 之间**不做断点续爬**，即 Stage 2 = Stage 1 + 遍历视频列表，一次性执行
- Stage 1 和 Stage 2 共用同一个 `authors.csv` 文件：
  - Stage 1 写入精简版 header + 数据行（部分列）
  - Stage 2 写入完整版 header + 数据行（所有列）
  - 如果用户选择 `--stage 2`，CSV 直接写完整版，不需要先写精简版再回填
  - 如果用户选择 `--stage 1`，CSV 只写精简版

#### R2: AuthorInfo 字段扩展（B 站平台层）

- 扩展 `FetchAuthorInfo` 拦截的 API 响应解析，从 B 站 API 中提取更多字段：
  - 总视频数量：来自 `/x/space/wbi/arc/search` 的 `data.page.count`（✅ 探针已验证）
  - 粉丝数（已有）：来自 `/x/relation/stat` 的 `data.follower`
  - 获赞数：来自 `/x/space/upstat` 的 `data.likes`（✅ 探针已验证）
  - 总播放数：来自 `/x/space/upstat` 的 `data.archive.view`（✅ 探针已验证）
- **探针验证结论**：
  - `/x/space/wbi/acc/info` **不返回** `archive_count`（视频数量），需要从 `arc/search` 的 `page.count` 获取
  - `/x/space/upstat` 结构已确认：`data.archive.view`（总播放数）、`data.likes`（总获赞数）
  - Stage 1 需要额外拦截 `arc/search`（只取第一页的 `page.count`，不遍历后续页）
- 平均播放量 = 总播放数 / 总视频数（从 API 汇总数据计算，不再遍历视频列表）
- 平均点赞量 = 获赞数 / 总视频数（从 API 汇总数据计算，注意：`TotalLikes` 包含视频+评论+动态的点赞，是近似值）

#### R3: Stage 2 统计字段

- 遍历视频列表时，计算以下统计量：
  - 平均评论数（已有，来自 `arc/search` 的 `comment` 字段）
  - 平均播放量（已有，来自 `arc/search` 的 `play` 字段）
  - 平均时长（已有，来自 `arc/search` 的 `length` 字段）
  - TOP3 视频（已有）
- **探针验证结论**：
  - ~~平均转发数~~：**已放弃**。视频列表 API 单个视频不返回 `share` 字段（需要点进视频详情页才能获取）
  - ~~平均收藏数~~：**已放弃**。视频列表 API 单个视频不返回 `favorite` 字段（同上）
  - ~~平均点赞数~~：**已放弃**。视频列表 API 单个视频不返回 `like` 字段（同上），当前 `LikeCount` 始终为 0
  - 视频列表 API 可用字段：`play`（播放）、`comment`（评论）、`video_review`（弹幕数）、`length`（时长）、`created`（发布时间）

#### R4: 平台差异化 Header（方案 A：放到平台包里）

- 将 CSV header 定义和对应的数据结构体从 `src/types.go` + `src/export/export.go` 迁移到平台包（如 `src/platform/bilibili/`）
- 每个平台定义自己的：
  - Video CSV header + row 转换函数
  - Author CSV header（精简版 + 完整版）+ row 转换函数
  - 平台特有的数据结构体（如 `BilibiliAuthorInfo`、`BilibiliAuthorStats`）
- 编排层保留通用的 CSV 写入流程（创建文件、写 BOM、写 header、写行、flush），但 header 和 row 数据由平台包提供
- 编排层通过接口或回调获取平台特定的 header 和 row 转换逻辑

### 3.2 非功能性需求

- **向后兼容**：Stage 0 的 CSV 输出格式不变
- **可扩展性**：新增平台时，只需在平台包中定义 header 和数据结构，编排层无需修改
- **代码可维护性**：平台特有逻辑集中在平台包内，编排层保持平台无关

## 4. 设计方案 (Design)

> 待系统设计阶段填写

### 4.1 方案概览

**核心变更**：将编排层从 2-stage（0→1）拆分为 3-stage（0→1→2），同时引入**平台 CSV 适配器**模式，让每个平台自定义 header/row 转换逻辑，编排层只负责通用的 CSV 写入流程。

**模块划分与变更范围**：

| 模块 | 变更类型 | 说明 |
|------|---------|------|
| `cmd/crawler/main.go` | 修改 | `--stage` 支持 `0\|1\|2\|all`，新增 Stage 2 调度逻辑，注入平台 CSV 适配器 |
| `src/crawler.go` | 修改 | 拆分 `RunStage1` → `RunStage1`（基本信息）+ `RunStage2`（完整信息）；`processOneAuthorOnce` 拆分为两个路径；Stage1Config/Stage2Config 接受 CSV 适配器 |
| `src/types.go` | 修改 | `AuthorInfo` 扩展字段（TotalLikes、TotalPlayCount、VideoCount）；新增 CSV 适配器接口定义 |
| `src/export/export.go` | 修改 | 移除硬编码的 `authorCSVHeader`/`authorToRow`；CSV Writer 改为接受外部传入的 header 和 row 转换函数 |
| `bilibili/types.go` | 修改 | `UserData` 扩展解析字段；新增 `UpStatResp` 结构体（拦截 `/x/space/upstat`） |
| `bilibili/author.go` | 修改 | `FetchAuthorInfo` 新增拦截 `/x/space/upstat` API，解析总播放数、获赞数 |
| `bilibili/csv.go` | **🆕 新建** | B 站平台的 CSV header 定义（精简版 + 完整版）和 row 转换函数，实现 CSV 适配器接口 |
| `src/stats/stats.go` | 修改 | `bilibiliVideoURLPrefix` 迁移到 bilibili 包；`videoURLPrefix` 改为参数传入 |
| `src/progress/progress.go` | 修改 | Stage 字段支持 0/1/2 |

**数据流**：

```
Stage 0: SearchPage → []Video → videos.csv + authors.json  (不变)

Stage 1: FetchAuthorInfo(扩展) → AuthorInfo(含总播放/获赞/视频数)
         → 直接计算平均播放量/点赞量 → 精简版 authors.csv

Stage 2: FetchAuthorInfo(扩展) → FetchAllAuthorVideos → CalcAuthorStats
         → 完整版 authors.csv (含平均评论/时长 + TOP3)
```

**依赖方向**：

```
main.go → src/crawler.go → src/types.go (接口定义)
                         → src/export/export.go (通用 Writer)
                         → src/stats/stats.go
                         → src/progress/progress.go

main.go → bilibili/csv.go → src/types.go (实现接口)
bilibili/author.go → bilibili/types.go → src/types.go
```

`bilibili/csv.go` 依赖 `src/types.go`（实现接口），`src/crawler.go` 依赖 `src/types.go`（使用接口），`main.go` 负责将 bilibili 的具体实现注入编排层。**无循环依赖**。

**关键 Trade-off**：

| 决策 | 选择 | 牺牲 | 理由 |
|------|------|------|------|
| Header 放平台包 vs 编排层 | 平台包 | 编排层无法直接看到列定义 | 不同平台列差异大，放编排层会膨胀 |
| CSV 适配器用接口 vs 函数类型 | 接口 | 稍多一点代码 | 语义更清晰，BasicHeader/FullHeader 天然分组 |
| Stage 1/2 不做断点续爬 | 不做 | Stage 2 中断需重跑 | 需求明确，简化实现 |
| 平均播放/点赞从 API 汇总计算 | API 汇总 | 与遍历计算结果可能有微小差异 | 更准确（覆盖全部视频），且 Stage 1 无需遍历 |
| `bilibiliVideoURLPrefix` 迁移到 bilibili 包 | 迁移 | stats 包不再负责 URL 生成 | 平台特有逻辑不应在通用包 |

### 4.2 组件设计 (Component Design)

#### 4.2.1 核心类/模块设计

本次重构涉及 5 个模块的变更和 1 个新模块的创建。按**依赖方向**（底层→上层）逐一说明。

##### (1) `src/types.go` — 通用数据模型 + CSV 适配器接口

**职责**：定义平台无关的数据结构和 CSV 适配器接口。编排层和平台层都依赖此模块。

**变更**：

```go
// AuthorInfo — 扩展 3 个字段（从 API 汇总数据获取）
type AuthorInfo struct {
    Name           string
    Followers      int64
    TotalLikes     int64  // 🆕 总获赞数
    TotalPlayCount int64  // 🆕 总播放数
    VideoCount     int    // 🆕 总视频数
}

// VideoDetail — 不变（探针确认视频列表 API 不返回 share/favorite/like）
// 移除原计划的 ShareCount/FavoriteCount 字段
type VideoDetail struct {
    // ... existing fields (PlayCount, CommentCount, LikeCount, Duration, etc.) ...
    // 注意：LikeCount 始终为 0（视频列表 API 不返回 like 字段）
    // 探针确认：视频列表 API 可用字段仅有 play、comment、video_review、length、created
}

// AuthorStats — 移除 AvgShareCount/AvgFavoriteCount（探针确认视频列表 API 不返回 share/favorite）
// 同时移除 AvgLikeCount（视频列表 API 不返回 like，始终为 0，无意义）
type AuthorStats struct {
    AvgPlayCount    float64
    AvgDuration     float64
    AvgCommentCount float64
}

// Author — 扩展 2 个字段（从 AuthorInfo 直接获取）
type Author struct {
    // ... existing fields ...
    TotalLikes     int64  // 🆕 总获赞数
    TotalPlayCount int64  // 🆕 总播放数
}
```

**新增 CSV 适配器接口**：

```go
// AuthorCSVAdapter defines platform-specific CSV header and row conversion for author data.
// Implemented by each platform package (e.g. bilibili/csv.go).
type AuthorCSVAdapter interface {
    // BasicHeader returns the CSV header for Stage 1 (basic author info, no video traversal).
    BasicHeader() []string
    // BasicRow converts an Author to a CSV row matching BasicHeader columns.
    BasicRow(author Author) []string
    // FullHeader returns the CSV header for Stage 2 (full author info with video stats).
    FullHeader() []string
    // FullRow converts an Author to a CSV row matching FullHeader columns.
    FullRow(author Author) []string
}

// VideoCSVAdapter defines platform-specific CSV header and row conversion for video data.
// Implemented by each platform package (e.g. bilibili/csv.go).
type VideoCSVAdapter interface {
    // Header returns the CSV header for video data.
    Header() []string
    // Row converts a Video to a CSV row matching Header columns.
    Row(video Video) []string
}
```

**设计决策**：
- `AuthorCSVAdapter` 分 Basic/Full 两组方法，对应 Stage 1/2 的不同列需求
- 接口方法只接受通用类型 `Author`/`Video`，平台包内部负责从通用类型提取平台特有字段
- `VideoCSVAdapter` 虽然当前 Stage 0 不变，但为了一致性也抽象为接口，便于未来平台扩展

##### (2) `bilibili/types.go` — B 站 API 响应结构体

**职责**：定义 B 站 API 的 JSON 响应结构体。

**变更**：

```go
// UserData — 扩展解析字段
type UserData struct {
    Name string `json:"name"`
    Sign string `json:"sign"`
}

// 🆕 UpStatResp — 拦截 /x/space/upstat API 的响应
// 该 API 返回 UP 主的总播放数和总获赞数
type UpStatResp struct {
    Code    int        `json:"code"`
    Message string     `json:"message"`
    Data    UpStatData `json:"data"`
}

type UpStatData struct {
    Archive UpStatArchive `json:"archive"`
    Likes   int64         `json:"likes"`
}

type UpStatArchive struct {
    View int64 `json:"view"` // 总播放数
}
```

**注意**：`/x/space/upstat` 的字段名已通过探针验证，上述结构与实际 API 返回一致。

##### (3) `bilibili/author.go` — B 站博主数据采集

**职责**：实现 `AuthorCrawler` 接口，通过浏览器拦截 API 获取博主信息和视频列表。

**变更**：

`FetchAuthorInfo` 新增拦截 `/x/space/upstat` 和 `/x/space/wbi/arc/search`：

```go
func (c *BiliBrowserAuthorCrawler) FetchAuthorInfo(ctx context.Context, mid string) (*src.AuthorInfo, error) {
    // ... existing code ...
    rules := []browser.InterceptRule{
        {URLPattern: "/x/space/wbi/acc/info", ID: "user_info"},
        {URLPattern: "/x/relation/stat", ID: "user_stat"},
        {URLPattern: "/x/space/upstat", ID: "up_stat"},           // 🆕
        {URLPattern: "/x/space/wbi/arc/search", ID: "video_list"}, // 🆕 只取 page.count
    }
    // ... intercept and parse ...
    // 🆕 Parse up stat for total play count and likes
    // 🆕 Parse video list for page.count (video count)
    return &src.AuthorInfo{
        Name:           infoResp.Data.Name,
        Followers:      statResp.Data.Follower,
        TotalLikes:     upStatResp.Data.Likes,          // 🆕
        TotalPlayCount: upStatResp.Data.Archive.View,   // 🆕
        VideoCount:     videoListResp.Data.Page.Count,   // 🆕 来自 arc/search 的 page.count
    }, nil
}
```

**探针验证结论**：
- `acc/info` 不返回 `archive_count`，`VideoCount` 改为从 `arc/search` 的 `data.page.count` 获取
- Stage 1 需要拦截 4 个 API（acc/info + relation/stat + upstat + arc/search）
- `arc/search` 在 Stage 1 中只用于获取 `page.count`，不解析视频列表内容
- Stage 2 中 `arc/search` 在 `FetchAuthorInfo` 拦截时只取 `page.count`，后续 `FetchAllAuthorVideos` 会重新遍历视频列表

`VideoListItem` **不扩展**（探针确认视频列表 API 不返回 share/favorite/like）：

```go
type VideoListItem struct {
    // 保持现有字段不变：title, bvid, play, comment, length, created
    // 探针确认：单个视频不返回 share/favorite/like/coin 字段
}
```

##### (4) `bilibili/csv.go` — 🆕 B 站 CSV 适配器

**职责**：实现 `AuthorCSVAdapter` 和 `VideoCSVAdapter` 接口，定义 B 站特有的 CSV 列和转换逻辑。

```go
package bilibili

import (
    "fmt"
    "strings"
    src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
)

// bilibiliVideoURLPrefix is the base URL for Bilibili video pages.
// Migrated from stats package — platform-specific URL logic belongs here.
const bilibiliVideoURLPrefix = "https://www.bilibili.com/video/"

// BilibiliAuthorCSVAdapter implements src.AuthorCSVAdapter for Bilibili platform.
type BilibiliAuthorCSVAdapter struct{}

// Compile-time interface check.
var _ src.AuthorCSVAdapter = (*BilibiliAuthorCSVAdapter)(nil)

func (a *BilibiliAuthorCSVAdapter) BasicHeader() []string {
    return []string{
        "博主名字", "ID", "粉丝数", "总获赞数", "总播放数", "视频数量",
        "视频平均播放量", "视频平均点赞量",
    }
}

func (a *BilibiliAuthorCSVAdapter) BasicRow(author src.Author) []string {
    avgPlay := safeDiv(float64(author.TotalPlayCount), float64(author.VideoCount))
    avgLike := safeDiv(float64(author.TotalLikes), float64(author.VideoCount))
    return []string{
        author.Name,
        author.ID,
        fmt.Sprintf("%d", author.Followers),
        fmt.Sprintf("%d", author.TotalLikes),
        fmt.Sprintf("%d", author.TotalPlayCount),
        fmt.Sprintf("%d", author.VideoCount),
        fmt.Sprintf("%.1f", avgPlay),
        fmt.Sprintf("%.1f", avgLike),
    }
}

func (a *BilibiliAuthorCSVAdapter) FullHeader() []string {
    return []string{
        "博主名字", "ID", "粉丝数", "总获赞数", "总播放数", "视频数量",
        "视频平均播放量", "视频平均点赞量",
        "视频平均评论数", "视频平均时长",
        "视频_TOP1", "视频_TOP2", "视频_TOP3",
    }
}
func (a *BilibiliAuthorCSVAdapter) FullRow(author src.Author) []string {
    avgPlay := safeDiv(float64(author.TotalPlayCount), float64(author.VideoCount))
    avgLike := safeDiv(float64(author.TotalLikes), float64(author.VideoCount))
    return []string{
        author.Name,
        author.ID,
        fmt.Sprintf("%d", author.Followers),
        fmt.Sprintf("%d", author.TotalLikes),
        fmt.Sprintf("%d", author.TotalPlayCount),
        fmt.Sprintf("%d", author.VideoCount),
        fmt.Sprintf("%.1f", avgPlay),
        fmt.Sprintf("%.1f", avgLike),
        fmt.Sprintf("%.1f", author.Stats.AvgCommentCount),
        fmt.Sprintf("%.1f", author.Stats.AvgDuration),
        topVideoHyperlink(author.TopVideos, 0),
        topVideoHyperlink(author.TopVideos, 1),
        topVideoHyperlink(author.TopVideos, 2),
    }
}

// safeDiv returns a/b, or 0 if b is 0.
func safeDiv(a, b float64) float64 {
    if b == 0 {
        return 0
    }
    return a / b
}

// topVideoHyperlink generates an Excel HYPERLINK formula for the i-th top video.
// Migrated from export package — platform-specific URL format belongs here.
func topVideoHyperlink(topVideos []src.TopVideo, index int) string {
    if index >= len(topVideos) {
        return ""
    }
    v := topVideos[index]
    title := strings.ReplaceAll(v.Title, "\"", "\"\"")
    return fmt.Sprintf(`=HYPERLINK("%s","%s")`, v.URL, title)
}

// BilibiliVideoCSVAdapter implements src.VideoCSVAdapter for Bilibili platform.
type BilibiliVideoCSVAdapter struct{}

var _ src.VideoCSVAdapter = (*BilibiliVideoCSVAdapter)(nil)

func (a *BilibiliVideoCSVAdapter) Header() []string {
    return []string{
        "标题", "作者", "AuthorID", "播放次数", "发布时间", "视频时长(s)", "来源",
    }
}

func (a *BilibiliVideoCSVAdapter) Row(video src.Video) []string {
    return []string{
        video.Title,
        video.Author,
        video.AuthorID,
        fmt.Sprintf("%d", video.PlayCount),
        video.PubDate.Format("2006-01-02 15:04:05"),
        fmt.Sprintf("%d", video.Duration),
        video.Source,
    }
}
```

**设计决策**：
- Stage 1 的平均播放量/点赞量在 `BasicRow` 中**实时计算**（`TotalPlayCount / VideoCount`），不依赖 `AuthorStats`
- Stage 2 的平均播放量/点赞量同样从 API 汇总数据计算（与 Stage 1 一致），平均评论/时长从遍历视频列表计算
- 探针确认：视频列表 API 不返回 share/favorite/like，已从 FullHeader/FullRow 中移除这三个字段
- `topVideoHyperlink` 和 `bilibiliVideoURLPrefix` 从 `export` 和 `stats` 包迁移到此处
- Video CSV 适配器当前与原有 header 完全一致（向后兼容）

##### (5) `src/export/export.go` — 通用 CSV Writer

**职责**：提供通用的 CSV 文件创建、BOM 写入、增量写入能力。不再包含 header 和 row 转换逻辑。

**变更**：

```go
// AuthorCSVWriter — 改为接受外部传入的 header 和 row 转换函数
type AuthorCSVWriter struct {
    f       *os.File
    w       *csv.Writer
    mu      sync.Mutex
    path    string
    toRow   func(src.Author) []string  // 🆕 由平台适配器提供
}

// NewAuthorCSVWriter — 新增 header 和 toRow 参数
func NewAuthorCSVWriter(outputDir, platform, keyword string,
    header []string, toRow func(src.Author) []string) (*AuthorCSVWriter, error) {
    // ... create file, write BOM, write header ...
    return &AuthorCSVWriter{f: f, w: w, path: absPath, toRow: toRow}, nil
}

// WriteRow — 使用注入的 toRow 函数
func (aw *AuthorCSVWriter) WriteRow(author src.Author) error {
    // ... lock ...
    if err := aw.w.Write(aw.toRow(author)); err != nil { ... }
    // ... flush ...
}

// VideoCSVWriter — 同理改为接受外部传入的 header 和 toRow
type VideoCSVWriter struct {
    f     *os.File
    w     *csv.Writer
    mu    sync.Mutex
    path  string
    toRow func(src.Video) []string  // 🆕
}
```

**移除**：
- `var videoCSVHeader` → 迁移到 `bilibili/csv.go`
- `func videoToRow` → 迁移到 `bilibili/csv.go`
- `var authorCSVHeader` → 迁移到 `bilibili/csv.go`
- `func authorToRow` → 迁移到 `bilibili/csv.go`
- `func topVideoHyperlink` → 迁移到 `bilibili/csv.go`

**保留**：
- `GenerateFileName`、`ReadVideoCSV`、`ReadCompletedAuthors` — 通用逻辑不变
- BOM 写入、文件创建、增量 flush — 通用流程不变

##### (6) `src/crawler.go` — 编排层

**职责**：Stage 0/1/2 的编排逻辑。通过接口和配置注入与平台解耦。

**变更**：

```go
// Stage1Config — 移除视频遍历相关配置，新增 CSV 适配器
type Stage1Config struct {
    Platform               string
    Keyword                string
    OutputDir              string
    Concurrency            int
    MaxConsecutiveFailures int
    RequestInterval        time.Duration
    Progress               ProgressTracker
    PoolRun                PoolRunFunc[AuthorMid, Author]
    NewAuthorCSVWriter     func(outputDir, platform, keyword string) (AuthorCSVRowWriter, error)
    OpenAuthorCSVWriter    func(existingPath string) (AuthorCSVRowWriter, error)
    ExistingCSVPath        string
    // 🆕 不再需要 CalcAuthorStats、MaxVideoPerAuthor、VideoPageSize
    // 🆕 不再需要 Cooldown（Stage 1 只调用 FetchAuthorInfo，风控风险低）
}

// 🆕 Stage2Config — 完整博主信息，包含视频遍历
type Stage2Config struct {
    Platform               string
    Keyword                string
    OutputDir              string
    Concurrency            int
    MaxVideoPerAuthor      int
    MaxConsecutiveFailures int
    RequestInterval        time.Duration
    Progress               ProgressTracker
    PoolRun                PoolRunFunc[AuthorMid, Author]
    NewAuthorCSVWriter     func(outputDir, platform, keyword string) (AuthorCSVRowWriter, error)
    OpenAuthorCSVWriter    func(existingPath string) (AuthorCSVRowWriter, error)
    ExistingCSVPath        string
    CalcAuthorStats        func(videos []VideoDetail, topN int) (AuthorStats, []TopVideo)
    Cooldown               *pool.Cooldown
}
```

**RunStage1（精简版）**：

```go
func RunStage1(ctx context.Context, ac AuthorCrawler, mids []AuthorMid, cfg Stage1Config) error {
    // ... create CSV writer ...
    // Worker Pool: for each author → FetchAuthorInfo only → write basic row
    // No FetchAllAuthorVideos, no CalcAuthorStats
}
```

**RunStage2（完整版）**：

```go
func RunStage2(ctx context.Context, ac AuthorCrawler, mids []AuthorMid, cfg Stage2Config) error {
    // ... create CSV writer ...
    // Worker Pool: for each author → FetchAuthorInfo + FetchAllAuthorVideos + CalcAuthorStats → write full row
    // Includes cooldown/retry logic (same as current RunStage1)
}
```

**processOneAuthorOnce 拆分**：

```go
// processOneAuthorBasic — Stage 1: only FetchAuthorInfo
func processOneAuthorBasic(ctx context.Context, ac AuthorCrawler, mid AuthorMid, cfg Stage1Config) (Author, error) {
    info, err := ac.FetchAuthorInfo(ctx, mid.ID)
    // ... no video fetching, no stats calculation ...
    return Author{
        Name: info.Name, ID: mid.ID, Followers: info.Followers,
        VideoCount: info.VideoCount,
        TotalLikes: info.TotalLikes, TotalPlayCount: info.TotalPlayCount,
    }, nil
}

// processOneAuthorFull — Stage 2: FetchAuthorInfo + FetchAllAuthorVideos + CalcAuthorStats
func processOneAuthorFull(ctx context.Context, ac AuthorCrawler, mid AuthorMid, cfg Stage2Config) (Author, error) {
    // Same as current processOneAuthorOnce, but with extended fields
    info, err := ac.FetchAuthorInfo(ctx, mid.ID)
    // ... pause ...
    allVideos, pageInfo, err := ac.FetchAllAuthorVideos(ctx, mid.ID, cfg.MaxVideoPerAuthor)
    stats, topVideos := cfg.CalcAuthorStats(allVideos, 3)
    return Author{
        Name: info.Name, ID: mid.ID, Followers: info.Followers,
        VideoCount: pageInfo.TotalCount,
        TotalLikes: info.TotalLikes, TotalPlayCount: info.TotalPlayCount,
        Stats: stats, TopVideos: topVideos,
    }, nil
}
```

##### (7) `cmd/crawler/main.go` — CLI 入口

**变更**：

```go
// --stage 支持 0|1|2|all
stage := flag.String("stage", "all", "Stage to run: 0, 1, 2, or all")

// 创建平台 CSV 适配器
authorAdapter := &bilibili.BilibiliAuthorCSVAdapter{}
videoAdapter := &bilibili.BilibiliVideoCSVAdapter{}

// Stage 1 注入 BasicHeader/BasicRow
stage1Cfg := src.Stage1Config{
    NewAuthorCSVWriter: func(outputDir, platform, keyword string) (src.AuthorCSVRowWriter, error) {
        return export.NewAuthorCSVWriter(outputDir, platform, keyword,
            authorAdapter.BasicHeader(), authorAdapter.BasicRow)
    },
    // ...
}

// Stage 2 注入 FullHeader/FullRow
stage2Cfg := src.Stage2Config{
    NewAuthorCSVWriter: func(outputDir, platform, keyword string) (src.AuthorCSVRowWriter, error) {
        return export.NewAuthorCSVWriter(outputDir, platform, keyword,
            authorAdapter.FullHeader(), authorAdapter.FullRow)
    },
    // ...
}
```

#### 4.2.2 接口设计

##### CSV 适配器接口

```go
// AuthorCSVAdapter — 平台差异化的 Author CSV 转换
// 调用方：编排层（通过 Stage1Config/Stage2Config 间接使用）
// 实现方：bilibili/csv.go
// 并发安全：是（无状态，所有方法都是纯函数）
type AuthorCSVAdapter interface {
    BasicHeader() []string              // Stage 1 精简版列头
    BasicRow(author Author) []string    // Stage 1 精简版行数据
    FullHeader() []string               // Stage 2 完整版列头
    FullRow(author Author) []string     // Stage 2 完整版行数据
}

// VideoCSVAdapter — 平台差异化的 Video CSV 转换
// 调用方：编排层（通过 Stage0Config 间接使用）
// 实现方：bilibili/csv.go
// 并发安全：是（无状态）
type VideoCSVAdapter interface {
    Header() []string                   // 视频列头
    Row(video Video) []string           // 视频行数据
}
```

##### AuthorCrawler 接口（不变）

```go
type AuthorCrawler interface {
    FetchAuthorInfo(ctx context.Context, mid string) (*AuthorInfo, error)
    FetchAllAuthorVideos(ctx context.Context, mid string, maxVideos int) ([]VideoDetail, PageInfo, error)
}
```

`FetchAuthorInfo` 的返回值 `*AuthorInfo` 扩展了 3 个字段（TotalLikes、TotalPlayCount、VideoCount），但接口签名不变，**向后兼容**。

##### CSV Writer 接口（签名不变，内部实现变更）

```go
// AuthorCSVRowWriter — 签名不变，但 NewAuthorCSVWriter 构造函数新增参数
type AuthorCSVRowWriter interface {
    WriteRow(author Author) error
    FilePath() string
    Close() error
}

// VideoCSVRowWriter — 签名不变
type VideoCSVRowWriter interface {
    WriteRows(videos []Video) error
    FilePath() string
    Close() error
}
```

#### 4.2.3 数据模型

##### AuthorInfo（扩展后）

| 字段 | 类型 | 来源 | Stage 1 | Stage 2 |
|------|------|------|---------|---------|
| Name | string | `/x/space/wbi/acc/info` | ✅ | ✅ |
| Followers | int64 | `/x/relation/stat` | ✅ | ✅ |
| TotalLikes | int64 | `/x/space/upstat` 🆕 | ✅ | ✅ |
| TotalPlayCount | int64 | `/x/space/upstat` 🆕 | ✅ | ✅ |
| VideoCount | int | `/x/space/wbi/arc/search` 的 `page.count` 🆕 | ✅ | ✅ |

##### Author（扩展后）

| 字段 | 类型 | 来源 | Stage 1 | Stage 2 |
|------|------|------|---------|---------|
| Name | string | AuthorInfo | ✅ | ✅ |
| ID | string | AuthorMid | ✅ | ✅ |
| Followers | int64 | AuthorInfo | ✅ | ✅ |
| VideoCount | int | AuthorInfo | ✅ | ✅ |
| TotalLikes | int64 | AuthorInfo 🆕 | ✅ | ✅ |
| TotalPlayCount | int64 | AuthorInfo 🆕 | ✅ | ✅ |
| Stats | AuthorStats | CalcAuthorStats | ❌ | ✅ |
| TopVideos | []TopVideo | CalcAuthorStats | ❌ | ✅ |

##### B 站精简版 Author CSV（Stage 1，8 列）

| 列名 | 数据来源 |
|------|---------|
| 博主名字 | Author.Name |
| ID | Author.ID |
| 粉丝数 | Author.Followers |
| 总获赞数 | Author.TotalLikes |
| 总播放数 | Author.TotalPlayCount |
| 视频数量 | Author.VideoCount |
| 视频平均播放量 | TotalPlayCount / VideoCount |
| 视频平均点赞量 | TotalLikes / VideoCount |

##### B 站完整版 Author CSV（Stage 2，13 列）

| 列名 | 数据来源 |
|------|---------|
| 博主名字 | Author.Name |
| ID | Author.ID |
| 粉丝数 | Author.Followers |
| 总获赞数 | Author.TotalLikes |
| 总播放数 | Author.TotalPlayCount |
| 视频数量 | Author.VideoCount |
| 视频平均播放量 | TotalPlayCount / VideoCount |
| 视频平均点赞量 | TotalLikes / VideoCount |
| 视频平均评论数 | Stats.AvgCommentCount |
| 视频平均时长 | Stats.AvgDuration |
| 视频_TOP1 | TopVideos[0] HYPERLINK |
| 视频_TOP2 | TopVideos[1] HYPERLINK |
| 视频_TOP3 | TopVideos[2] HYPERLINK |

#### 4.2.4 并发模型

并发模型**不变**，沿用现有设计：

- **Stage 0**：Worker Pool 并发抓取搜索页，`VideoCSVWriter` 内部 `sync.Mutex` 保护写入
- **Stage 1**（新）：Worker Pool 并发抓取博主基本信息，`AuthorCSVWriter` 内部 `sync.Mutex` 保护写入。**无 Cooldown**（只调用 FetchAuthorInfo，风控风险较低）
- **Stage 2**（新）：Worker Pool 并发抓取博主完整信息，`AuthorCSVWriter` 内部 `sync.Mutex` 保护写入。**有 Cooldown**（遍历视频列表触发风控风险高，沿用现有 412 重试机制）

**变更点**：Stage 1 移除了 Cooldown 机制（因为不遍历视频列表，API 调用频率低）。Stage 2 保留完整的 Cooldown + 重试机制。

#### 4.2.5 错误处理

错误处理策略**基本不变**，沿用现有设计：

| 错误类型 | 处理方式 | 适用 Stage |
|----------|---------|-----------|
| 412 风控 | 全局 Cooldown + 指数退避重试（最多 3 次） | Stage 2 |
| 拦截超时 | 同 412 处理 | Stage 1, 2 |
| API 返回非 0 code | 不重试，记录错误，跳过该博主 | Stage 1, 2 |
| JSON 解析失败 | 不重试，记录错误，跳过该博主 | Stage 1, 2 |
| CSV 写入失败 | 记录 WARN，不中断流程 | Stage 0, 1, 2 |

**新增考虑**：
- `/x/space/upstat` 拦截失败（API 未触发）：**返回错误，不降级**。该 API 在浏览器中已确认必定触发，拦截失败属于异常情况，应直接报错
- Stage 2 的 `FetchAllAuthorVideos` 如果视频列表中缺少 share/favorites 字段，设为 0（与现有 LikeCount 处理方式一致）
### 4.3 核心逻辑实现

#### 4.3.1 重试逻辑抽取

**当前状态**：`processOneAuthor` 函数内嵌了 3 次重试 + 指数退避 + Cooldown 触发逻辑（crawler.go L195-L228）。

**改动**：抽取为通用泛型函数 `retryWithCooldown`，Stage 2 复用，Stage 1 不使用（Stage 1 只调用 FetchAuthorInfo，轻量操作无需重试）。

```go
// retryWithCooldown retries a function up to maxRetries times with exponential backoff.
// On retryable errors (412, intercept timeout), triggers global cooldown so all workers pause.
// Non-retryable errors are returned immediately without retry.
func retryWithCooldown[T any](
    ctx context.Context,
    maxRetries int,
    cd *pool.Cooldown,
    label string, // for logging, e.g. "author 张三 (mid=12345)"
    fn func() (T, error),
) (T, error) {
    var zero T
    var lastErr error

    for attempt := 0; attempt <= maxRetries; attempt++ {
        if attempt > 0 {
            backoff := time.Duration(10<<(attempt-1)) * time.Second
            if cd != nil {
                cd.Trigger(backoff)
            }
            log.Printf("WARN: Retrying %s in %v (attempt %d/%d, last error: %v)",
                label, backoff, attempt, maxRetries, lastErr)
            select {
            case <-ctx.Done():
                return zero, fmt.Errorf("context cancelled during retry backoff: %w", ctx.Err())
            case <-time.After(backoff):
            }
        }

        result, err := fn()
        if err == nil {
            return result, nil
        }
        lastErr = err

        if !isRetryableError(err) {
            return zero, err
        }
    }

    return zero, fmt.Errorf("all %d retries exhausted for %s: %w", maxRetries, label, lastErr)
}
```

**调用方式**：

```go
// Stage 1: 直接调用，无重试
author, err := processOneAuthorBasic(ctx, ac, mid)

// Stage 2: 通过 retryWithCooldown 包装
author, err := retryWithCooldown(ctx, 3, cd,
    fmt.Sprintf("author %s (mid=%s)", mid.Name, mid.ID),
    func() (Author, error) {
        return processOneAuthorFull(ctx, ac, mid, cfg)
    },
)
```

#### 4.3.2 Stage 拆分（编排层）

**`processOneAuthorBasic`（Stage 1）**：

```go
func processOneAuthorBasic(ctx context.Context, ac AuthorCrawler, mid AuthorMid) (Author, error) {
    info, err := ac.FetchAuthorInfo(ctx, mid.ID)
    if err != nil {
        return Author{}, fmt.Errorf("fetch author info: %w", err)
    }

    author := Author{
        Name:           info.Name,
        ID:             mid.ID,
        Followers:      info.Followers,
        VideoCount:     info.VideoCount,
        TotalLikes:     info.TotalLikes,
        TotalPlayCount: info.TotalPlayCount,
    }

    log.Printf("INFO: Author %s (mid=%s): followers=%d, videos=%d",
        info.Name, mid.ID, info.Followers, info.VideoCount)
    return author, nil
}
```

**`processOneAuthorFull`（Stage 2）**：

```go
func processOneAuthorFull(ctx context.Context, ac AuthorCrawler, mid AuthorMid, cfg Stage2Config) (Author, error) {
    authorStart := time.Now()

    // Step 1: Fetch author info (same as Stage 1).
    info, err := ac.FetchAuthorInfo(ctx, mid.ID)
    if err != nil {
        return Author{}, fmt.Errorf("fetch author info: %w", err)
    }

    // Brief pause between API calls.
    if cfg.RequestInterval > 0 {
        time.Sleep(pool.JitteredDuration(cfg.RequestInterval))
    }

    // Step 2: Fetch all videos.
    allVideos, pageInfo, err := ac.FetchAllAuthorVideos(ctx, mid.ID, cfg.MaxVideoPerAuthor)
    if err != nil {
        return Author{}, fmt.Errorf("fetch author videos: %w", err)
    }

    // Step 3: Calculate stats (extended with share/favorite).
    stats, topVideos := cfg.CalcAuthorStats(allVideos, 3)

    author := Author{
        Name:           info.Name,
        ID:             mid.ID,
        Followers:      info.Followers,
        VideoCount:     pageInfo.TotalCount,
        TotalLikes:     info.TotalLikes,
        TotalPlayCount: info.TotalPlayCount,
        Stats:          stats,
        TopVideos:      topVideos,
    }

    log.Printf("INFO: Author %s: %d videos fetched, %v",
        info.Name, len(allVideos), time.Since(authorStart).Round(time.Millisecond))
    return author, nil
}
```

**`RunStage1`（精简版）**：

```go
func RunStage1(ctx context.Context, ac AuthorCrawler, mids []AuthorMid, cfg Stage1Config) error {
    // ... create CSV writer (same pattern as current) ...

    // Worker Pool: no cooldown, no retry.
    results := cfg.PoolRun(ctx, cfg.Concurrency, mids,
        func(ctx context.Context, mid AuthorMid) (Author, error) {
            author, err := processOneAuthorBasic(ctx, ac, mid)
            if err != nil {
                return Author{}, err
            }
            if writeErr := csvWriter.WriteRow(author); writeErr != nil {
                log.Printf("WARN: failed to write author %s to CSV: %v", mid.ID, writeErr)
            }
            return author, nil
        },
        cfg.MaxConsecutiveFailures,
        cfg.RequestInterval,
    )
    // ... log results ...
}
```

**`RunStage2`（完整版）**：

```go
func RunStage2(ctx context.Context, ac AuthorCrawler, mids []AuthorMid, cfg Stage2Config) error {
    // ... create CSV writer (same pattern) ...

    cd := cfg.Cooldown

    results := cfg.PoolRun(ctx, cfg.Concurrency, mids,
        func(ctx context.Context, mid AuthorMid) (Author, error) {
            cd.Wait(ctx)
            if ctx.Err() != nil {
                return Author{}, ctx.Err()
            }

            // Use retryWithCooldown for 412 resilience.
            author, err := retryWithCooldown(ctx, 3, cd,
                fmt.Sprintf("author %s (mid=%s)", mid.Name, mid.ID),
                func() (Author, error) {
                    return processOneAuthorFull(ctx, ac, mid, cfg)
                },
            )
            if err != nil {
                return Author{}, err
            }

            if writeErr := csvWriter.WriteRow(author); writeErr != nil {
                log.Printf("WARN: failed to write author %s to CSV: %v", mid.ID, writeErr)
            }
            return author, nil
        },
        cfg.MaxConsecutiveFailures,
        cfg.RequestInterval,
    )
    // ... log results ...
}
```

#### 4.3.3 `FetchAuthorInfo` 扩展

**改动**：新增拦截 `/x/space/upstat` 和 `/x/space/wbi/arc/search`，解析总播放数、获赞数和视频数量。四个 API 都必须成功拦截，任一失败直接返回错误。

```go
func (c *BiliBrowserAuthorCrawler) FetchAuthorInfo(ctx context.Context, mid string) (*src.AuthorInfo, error) {
    page := c.manager.GetPage()
    defer c.manager.PutPage(page)

    targetURL := fmt.Sprintf("https://space.bilibili.com/%s", mid)

    rules := []browser.InterceptRule{
        {URLPattern: "/x/space/wbi/acc/info", ID: "user_info"},
        {URLPattern: "/x/relation/stat", ID: "user_stat"},
        {URLPattern: "/x/space/upstat", ID: "up_stat"},           // 🆕
        {URLPattern: "/x/space/wbi/arc/search", ID: "video_list"}, // 🆕 只取 page.count
    }

    results, err := browser.NavigateAndIntercept(ctx, page, targetURL, rules)
    if err != nil {
        return nil, fmt.Errorf("fetch author info mid=%s: %w", mid, err)
    }

    // Extract response bodies.
    var infoBody, statBody, upStatBody, videoListBody []byte
    for _, r := range results {
        switch r.ID {
        case "user_info":
            infoBody = r.Body
        case "user_stat":
            statBody = r.Body
        case "up_stat":
            upStatBody = r.Body
        case "video_list":
            videoListBody = r.Body
        }
    }

    // All four APIs must be intercepted — no degradation.
    if infoBody == nil { return nil, fmt.Errorf("user info API not intercepted for mid=%s", mid) }
    if statBody == nil { return nil, fmt.Errorf("user stat API not intercepted for mid=%s", mid) }
    if upStatBody == nil { return nil, fmt.Errorf("up stat API not intercepted for mid=%s", mid) }
    if videoListBody == nil { return nil, fmt.Errorf("video list API not intercepted for mid=%s", mid) }

    // Parse user info.
    var infoResp UserInfoResp
    // ... unmarshal + code check ...

    // Parse user stat.
    var statResp UserStatResp
    // ... unmarshal + code check ...

    // 🆕 Parse up stat.
    var upStatResp UpStatResp
    if err := json.Unmarshal(upStatBody, &upStatResp); err != nil {
        return nil, fmt.Errorf("parse up stat mid=%s: %w", mid, err)
    }
    if upStatResp.Code != 0 {
        return nil, fmt.Errorf("up stat API error mid=%s (code=%d)", mid, upStatResp.Code)
    }

    // 🆕 Parse video list (only for page.count).
    var videoListResp VideoListResp
    if err := json.Unmarshal(videoListBody, &videoListResp); err != nil {
        return nil, fmt.Errorf("parse video list mid=%s: %w", mid, err)
    }
    if videoListResp.Code != 0 {
        return nil, fmt.Errorf("video list API error mid=%s (code=%d)", mid, videoListResp.Code)
    }

    return &src.AuthorInfo{
        Name:           infoResp.Data.Name,
        Followers:      statResp.Data.Follower,
        TotalLikes:     upStatResp.Data.Likes,
        TotalPlayCount: upStatResp.Data.Archive.View,
        VideoCount:     videoListResp.Data.Page.Count, // 来自 arc/search 的 page.count（探针已验证）
    }, nil
}
```

#### 4.3.4 `CalcAuthorStats` 扩展

**改动**：移除 AvgShareCount/AvgFavoriteCount/AvgLikeCount（探针确认视频列表 API 不返回这些字段）；`bilibiliVideoURLPrefix` 迁移到 bilibili 包，`CalcAuthorStats` 的 `videoURLPrefix` 改为参数传入。

```go
// CalcAuthorStats — 精简后（移除无法获取的字段）
func CalcAuthorStats(videos []src.VideoDetail, topN int, videoURLPrefix string) (src.AuthorStats, []src.TopVideo) {
    if len(videos) == 0 {
        return src.AuthorStats{}, nil
    }

    var totalPlay, totalComment int64
    var totalDuration int

    for _, v := range videos {
        totalPlay += v.PlayCount
        totalDuration += v.Duration
        totalComment += v.CommentCount
    }

    count := float64(len(videos))
    stats := src.AuthorStats{
        AvgPlayCount:    float64(totalPlay) / count,
        AvgDuration:     float64(totalDuration) / count,
        AvgCommentCount: float64(totalComment) / count,
    }

    // ... sort + top N (same logic, but use videoURLPrefix parameter) ...
    topVideos[i] = src.TopVideo{
        Title:     sorted[i].Title,
        URL:       fmt.Sprintf("%s%s", videoURLPrefix, sorted[i].BvID),
        PlayCount: sorted[i].PlayCount,
    }

    return stats, topVideos
}
```

**关键变更**：`videoURLPrefix` 从硬编码常量改为函数参数，由调用方（`main.go`）传入。这样 `stats` 包不再依赖任何平台特有常量。

**调用方式（main.go）**：

```go
stage2Cfg := src.Stage2Config{
    CalcAuthorStats: func(videos []src.VideoDetail, topN int) (src.AuthorStats, []src.TopVideo) {
        return stats.CalcAuthorStats(videos, topN, bilibili.VideoURLPrefix)
    },
}
```

#### 4.3.5 CSV 适配器注入

**`NewAuthorCSVWriter` 签名变更**：

```go
// Before:
func NewAuthorCSVWriter(outputDir, platform, keyword string) (*AuthorCSVWriter, error)

// After:
func NewAuthorCSVWriter(outputDir, platform, keyword string,
    header []string, toRow func(src.Author) []string) (*AuthorCSVWriter, error)
```

**`main.go` 注入方式**：

```go
authorAdapter := &bilibili.BilibiliAuthorCSVAdapter{}

// Stage 1: inject BasicHeader/BasicRow
stage1Cfg := src.Stage1Config{
    NewAuthorCSVWriter: func(outputDir, platform, keyword string) (src.AuthorCSVRowWriter, error) {
        return export.NewAuthorCSVWriter(outputDir, platform, keyword,
            authorAdapter.BasicHeader(), authorAdapter.BasicRow)
    },
}

// Stage 2: inject FullHeader/FullRow
stage2Cfg := src.Stage2Config{
    NewAuthorCSVWriter: func(outputDir, platform, keyword string) (src.AuthorCSVRowWriter, error) {
        return export.NewAuthorCSVWriter(outputDir, platform, keyword,
            authorAdapter.FullHeader(), authorAdapter.FullRow)
    },
}
```

**`ReadCompletedAuthors` 不受影响**：该函数只读 column index 1（ID），不依赖 header 内容，所以 header 列数变化不影响断点续爬。

#### 4.3.6 `--stage` 参数语义

**`main.go` 调度逻辑**：

```go
runStage0 := *stage == "0" || *stage == "all"
runStage1 := *stage == "1" || *stage == "all"
runStage2 := *stage == "2" || *stage == "all"

if runStage0 { ... }  // 输出 videos.csv

if runStage1 && !runStage2 {
    // Stage 1 only: 精简版 author CSV
    src.RunStage1(ctx, authorCrawler, mids, stage1Cfg)
}

if runStage2 {
    // Stage 2 隐含 Stage 1: 完整版 author CSV
    // 内部先调 FetchAuthorInfo 再遍历视频列表
    src.RunStage2(ctx, authorCrawler, mids, stage2Cfg)
}
```

**`--stage all`** = Stage 0 → Stage 2（跳过独立的 Stage 1，因为 Stage 2 已包含 Stage 1 的全部数据）。

#### 4.3.7 边界情况汇总

| 边界情况 | 处理方式 |
|---------|----------|
| Stage 1 的 `FetchAuthorInfo` 拦截 `/x/space/upstat` 失败 | 返回错误，不降级 |
| Stage 1 不使用重试机制 | FetchAuthorInfo 是轻量操作，失败直接跳过该博主 |
| Stage 2 视频列表缺少 share/favorites/like 字段 | 已放弃这三个字段（探针确认视频列表 API 不返回） |
| `VideoCount` 来源 | 来自 `arc/search` 的 `data.page.count`（探针已验证） |
| `--stage all` 时 Stage 1 和 Stage 2 的关系 | `all` = Stage 0 + Stage 2，不单独跑 Stage 1 |
| 断点续爬：Stage 1 CSV 存在时跑 Stage 2 | 不支持跨 Stage 续爬，Stage 2 创建新 CSV |
| `CalcAuthorStats` 的 `videoURLPrefix` 参数化 | 由 `main.go` 闭包注入，`stats` 包不依赖平台常量 |
| `ReadCompletedAuthors` 读取 ID 列 | 读 column index 1（ID 在第二列），header 变化不影响 |

### 4.4 方案优劣分析

## 5. 备选方案 (Alternatives Considered)

> 待系统设计阶段填写

## 6. 业界调研 (Industry Research)

> 待系统设计阶段填写

## 7. 测试计划 (Test Plan)

> 待系统设计阶段填写

## 8. 可观测性 & 运维 (Observability & Operations)

> 待系统设计阶段填写

## 9. Changelog

| 日期 | 变更内容 | 作者 |
|------|----------|------|
| 2026-03-28 | 初始版本：需求澄清完成，前三章节 | AI |
| 2026-03-28 | 4.1 方案概览 + 4.2 组件设计 | AI |
| 2026-03-28 | 删除 bilibili author 的地区/语言字段（代码 + 文档） | AI |
| 2026-03-28 | 4.3 核心逻辑实现；修正 4.2.5 upstat 错误处理策略（返回错误，不降级） | AI |
| 2026-03-28 | 探针结果固化：移除 AvgShareCount/AvgFavoriteCount/AvgLikeCount；VideoCount 来源确认为 arc/search 的 page.count；Stage 2 CSV 从 14 列精简为 13 列 | AI |

## 10. 参考资料 (References)

- B 站 API 文档（非官方）：https://github.com/SocialSisterYi/bilibili-API-collect
