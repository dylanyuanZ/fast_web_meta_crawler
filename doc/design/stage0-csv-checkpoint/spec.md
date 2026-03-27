# Feature: Stage0 视频 CSV 实时落盘与断点续爬

**作者**: AI  
**日期**: 2026-03-27  
**状态**: Draft

---

## 1. 背景 (Background)

### 1.1 问题描述

Stage 0 搜索视频时，`pool.Run` 并发搜索各页，每页完成后 `AddSearchPage` 记录到 progress。但视频数据（`[]Video`）只存在于内存的 `allVideos` 中，`WriteVideoCSV` 在所有页面搜索完后才一次性写入。

如果中途中断（如搜索 50 页，已完成 40 页时 Ctrl+C）：
- `progress.json` 记录了 40 个已完成页码 ✅
- 但 40 页的视频数据全在内存中，**未落盘** ❌
- 恢复后只搜索剩余 10 页（跳过已完成页），但前 40 页的视频数据丢失
- 最终视频 CSV 只有 10 页的数据，中间数据文件（author mids）也只有 10 页的博主

**核心问题**：与 Stage 1 相同——断点续爬"续"了进度，但丢了数据。

### 1.2 现状分析

**Stage 0 数据流**：

1. `SearchPage(keyword, 1)` → 获取第一页 + 总页数
2. `pool.Run` 并发搜索剩余页 → 每页完成后 `AddSearchPage` 记录进度
3. 所有页完成后 → 收集 `allVideos` → `WriteVideoCSV` **一次性写入** CSV
4. 去重 → `writeIntermediateData` 写入 author mids JSON
5. `SetAuthorMids` 更新 progress 到 stage 1

**问题根因**：视频 CSV 写入时机太晚（仅在所有页面完成后），中断时数据全部丢失。

## 2. 目标 (Goals)

**核心目标**：Stage 0 的视频 CSV 也实时落盘，每搜索完一页就立即追加写入。

具体而言：
1. 每搜索完一页视频后，立即将该页视频追加写入 CSV 文件（实时落盘）
2. 中断后恢复时，已搜索页的视频数据在 CSV 中完整保留
3. 恢复时从 CSV 读取已有视频数据，用于去重生成 author mids
4. `progress.json` 中记录视频 CSV 文件路径

### 2.1 非目标 (Non-Goals)

- **不改 pool.Run 接口**

## 3. 需求细化 (Requirements)

### 3.1 功能性需求

1. **每页搜索完成后立即追加写入视频 CSV**：实时落盘
2. **恢复时从视频 CSV 读取已有视频**：用于去重生成 author mids（替代内存中的 `allVideos`）
3. **progress.json 中记录视频 CSV 文件路径**：恢复时定位 CSV 文件
4. **最终视频 CSV 完整**：包含所有页面的视频数据，无论是否经历过中断

### 3.2 非功能性需求

1. **并发安全**：多个 worker 可能同时完成搜索页，需要锁串行化 CSV 写入

## 4. 设计方案 (Design)

### 4.1 方案概览

**一句话**：引入 `VideoCSVWriter` 组件（复用 Stage 1 的 `AuthorCSVWriter` 模式），在 `RunStage0` 的 worker 回调中每搜索完一页就追加写入视频 CSV；恢复时从 CSV 读取已有视频用于去重。

#### 模块划分与改动范围

| 模块 | 文件 | 改动类型 | 说明 |
|------|------|---------|------|
| CSV 导出 | `src/export/export.go` | **新增** | 新增 `VideoCSVWriter` 结构体（复用 AuthorCSVWriter 模式），提供 `WriteRows([]Video)` 批量追加写入；新增 `ReadVideoCSV(csvPath)` 从 CSV 读取已有视频 |
| 进度管理 | `src/progress/progress.go` | **修改** | 新增 `VideoCSVPath string` 字段和 `SetVideoCSVPath` 方法 |
| Stage0 编排 | `src/crawler.go` | **修改** | `RunStage0` 改为：先创建 `VideoCSVWriter` → worker 回调中每页完成后调用 `WriteRows` → 结束后 `Close()`；`Stage0Config` 调整；`ProgressTracker` 接口新增 `SetVideoCSVPath` |
| 入口 | `cmd/crawler/main.go` | **修改** | Stage 0 恢复逻辑：从 CSV 读取已有视频用于去重 |

#### 数据流（改造后）

```
首次运行:
  ├─ NewVideoCSVWriter(outputDir, platform, keyword) → 创建新文件 + 写 BOM + header
  ├─ progress.SetVideoCSVPath(csvPath) → 记录 CSV 路径到 progress.json
  ├─ SearchPage(keyword, 1) → 第一页视频
  ├─ csvWriter.WriteRows(firstVideos) → 立即写入第一页
  ├─ pool.Run(workers)
  │    └─ 每个 worker:
  │         ├─ SearchPage(keyword, page) → 获取该页视频
  │         └─ csvWriter.WriteRows(videos) → 🔒 加锁追加写入 CSV（实时落盘）
  ├─ pool.Run 返回 results
  ├─ csvWriter.Close()
  ├─ 从 CSV 读取全部视频 → 去重 → author mids
  ├─ writeIntermediateData(mids)
  └─ SetAuthorMids(mids)

恢复时（Stage 0 中断后）:
  ├─ progress.VideoCSVPath → 定位已有视频 CSV
  ├─ OpenVideoCSVWriter(existingCSVPath) → 打开已有文件（追加模式）
  ├─ pool.Run(workers) → 仅搜索未完成的页（已有 CompletedPages 机制）
  │    └─ 每个 worker: SearchPage → csvWriter.WriteRows（追加到同一文件）
  ├─ csvWriter.Close()
  ├─ ReadVideoCSV(csvPath) → 读取全部视频 → 去重 → author mids
  └─ 后续同首次运行
```

#### 关键 trade-off

| 决策 | 选择 | 牺牲 | 理由 |
|------|------|------|------|
| CSV 写入时机 | 每页完成后立即写入 | 写入频率增加 | 核心目标：数据不丢失 |
| 去重时机 | 所有页完成后从 CSV 读取再去重 | 需要额外读一次 CSV | 简单可靠，避免在写入时维护去重状态 |
| 并发控制 | `sync.Mutex` 串行化写入 | 微小锁竞争 | 搜索页数据量小，锁开销可忽略 |

### 4.2 组件设计

#### 4.2.1 核心模块设计

##### 1. `VideoCSVWriter`（新增，`src/export/export.go`）

**职责**：持有视频 CSV 文件句柄，提供并发安全的批量追加写入能力。

```go
type VideoCSVWriter struct {
    f    *os.File
    w    *csv.Writer
    mu   sync.Mutex
    path string
}
```

**两种构造方式**：
- `NewVideoCSVWriter(outputDir, platform, keyword)`：首次运行，创建新文件 + 写 BOM + 写 header
- `OpenVideoCSVWriter(existingPath)`：恢复运行，追加模式打开

**与 AuthorCSVWriter 的区别**：
- `WriteRows([]Video)` 批量写入（一页可能有多条视频），而非单条
- 视频 CSV 的 header 不同（标题、作者、播放次数等）

##### 2. `ReadVideoCSV`（新增，`src/export/export.go`）

**职责**：从视频 CSV 文件读取全部视频数据，用于去重生成 author mids。

返回 `[]Video`（仅需 `AuthorID` 和 `Author` 字段用于去重）。

##### 3. `Progress` 结构体扩展（`src/progress/progress.go`）

- **新增** `VideoCSVPath string` 字段（JSON tag: `video_csv_path`）
- **新增** `SetVideoCSVPath(outputDir, csvPath string) error` 方法

##### 4. `ProgressTracker` 接口扩展（`src/crawler.go`）

- **新增** `SetVideoCSVPath(outputDir string, csvPath string) error`

##### 5. `Stage0Config` 改造（`src/crawler.go`）

- **删除** `WriteVideoCSV` 函数字段
- **新增** `NewVideoCSVWriter func(outputDir, platform, keyword string) (VideoCSVRowWriter, error)` 函数字段
- **新增** `OpenVideoCSVWriter func(existingPath string) (VideoCSVRowWriter, error)` 函数字段
- **新增** `ReadVideoCSV func(csvPath string) ([]Video, error)` 函数字段
- **新增** `ExistingVideoCSVPath string` 字段

##### 6. `RunStage0` 改造（`src/crawler.go`）

核心逻辑变更：

```go
func RunStage0(ctx, sc, keyword, cfg) ([]AuthorMid, error) {
    // 1. 创建或打开 VideoCSVWriter
    var csvWriter VideoCSVRowWriter
    if cfg.ExistingVideoCSVPath != "" {
        csvWriter, err = cfg.OpenVideoCSVWriter(cfg.ExistingVideoCSVPath)
    } else {
        csvWriter, err = cfg.NewVideoCSVWriter(cfg.OutputDir, cfg.Platform, keyword)
    }
    defer csvWriter.Close()

    // 2. 记录 CSV 路径到 progress
    cfg.Progress.SetVideoCSVPath(cfg.OutputDir, csvWriter.FilePath())

    // 3. 搜索第一页 + 写入 CSV
    firstVideos, pageInfo, err := sc.SearchPage(ctx, keyword, 1)
    csvWriter.WriteRows(firstVideos)

    // 4. pool.Run 搜索剩余页 — worker 中写入 CSV
    results := cfg.PoolRun(ctx, cfg.Concurrency, remainingPages,
        func(ctx, page) ([]Video, error) {
            videos, _, err := sc.SearchPage(ctx, keyword, page)
            csvWriter.WriteRows(videos)  // 实时落盘
            return videos, nil
        }, ...)

    // 5. csvWriter.Close()
    // 6. 从 CSV 读取全部视频 → 去重 → author mids
    allVideos, err := cfg.ReadVideoCSV(csvWriter.FilePath())
    // ... 去重逻辑同原来
}
```

#### 4.2.2 接口设计

##### `VideoCSVWriter` 公开 API

```go
func NewVideoCSVWriter(outputDir, platform, keyword string) (*VideoCSVWriter, error)
func OpenVideoCSVWriter(existingPath string) (*VideoCSVWriter, error)
func (w *VideoCSVWriter) WriteRows(videos []Video) error  // 批量写入
func (w *VideoCSVWriter) FilePath() string
func (w *VideoCSVWriter) Close() error
func ReadVideoCSV(csvPath string) ([]Video, error)
```

##### `VideoCSVRowWriter` 接口（`src/crawler.go`，避免循环导入）

```go
type VideoCSVRowWriter interface {
    WriteRows(videos []Video) error
    FilePath() string
    Close() error
}
```

##### `ProgressTracker` 接口（扩展后）

```go
type ProgressTracker interface {
    CompletedPages() map[int]bool
    AddSearchPage(outputDir string, page int) error
    SetAuthorMids(outputDir string, mids []AuthorMid) error
    SetAuthorCSVPath(outputDir string, csvPath string) error
    SetVideoCSVPath(outputDir string, csvPath string) error  // 新增
}
```

#### 4.2.3 数据模型

##### Video CSV Schema（不变）

```
标题, 作者, 播放次数, 发布时间, 视频时长(s), 来源
```

注意：Video CSV 不需要新增 ID 列，因为恢复时不需要按 ID 去重（去重是在读取全部视频后按 AuthorID 做的）。但 `ReadVideoCSV` 需要能解析出 `Author` 和 `AuthorID` 字段。

**问题**：现有 Video CSV header 中没有 `AuthorID` 列！只有"作者"（Author name）。去重需要 `AuthorID`。

**方案**：Video CSV header 新增 `AuthorID` 列（第 7 列）：
```
标题, 作者, AuthorID, 播放次数, 发布时间, 视频时长(s), 来源
```

##### `progress.json` Schema 变更

新增字段：`video_csv_path`（string，视频 CSV 文件路径）

#### 4.2.4 并发模型

与 Stage 1 的 AuthorCSVWriter 完全一致：`sync.Mutex` 串行化写入。

#### 4.2.5 错误处理

与 Stage 1 的 AuthorCSVWriter 完全一致。`WriteRows` 失败打 WARN 日志，不中断 worker。
