# Feature: Stage1 CSV 实时落盘与断点续爬

**作者**: AI  
**日期**: 2026-03-27  
**状态**: Draft

---

## 1. 背景 (Background)

### 1.1 问题描述

Stage1 处理大量博主时（如 600+ 个博主，耗时 2 小时+），如果中途 Ctrl+C 中断程序：

- `progress.json` 中的 `done_authors` 记录了哪些博主已完成
- 但博主的实际数据（Author 结构体）**从未落盘**——它们只存在于 `pool.Run` 返回的内存 `results` 中
- `WriteAuthorCSV` 只在 `RunStage1` 末尾调用一次，中断时 CSV 未写入
- 恢复后 `PendingAuthors()` 跳过已完成博主（不再抓取），导致最终 CSV **缺失这些博主数据**

**核心问题**：断点续爬"续"了进度，但丢了数据。

### 1.2 现状分析

**相关模块**：

| 模块 | 文件 | 职责 |
|------|------|------|
| Stage1 编排 | `src/crawler.go` | `RunStage1` 调用 pool.Run 并发处理博主，所有完成后一次性写 CSV |
| CSV 导出 | `src/export/export.go` | `WriteAuthorCSV` 使用 `os.Create` 全量覆盖写入，文件名含时间戳 |
| 进度管理 | `src/progress/progress.go` | `MarkDone` 每完成一个博主写入 progress.json（含 done_authors map） |
| Worker Pool | `src/pool/pool.go` | 所有任务完成后才返回 `[]TaskResult` |
| 入口 | `cmd/crawler/main.go` | 恢复时调用 `prog.PendingAuthors()` 过滤已完成博主 |

**现有数据流**：

1. `pool.Run` 并发处理博主 → 每个博主完成后 `MarkDone` 写 progress.json
2. 所有博主完成后 → `pool.Run` 返回 `[]TaskResult`
3. 收集成功结果 → `WriteAuthorCSV` 一次性写入 CSV
4. 任务完成 → 删除 progress.json

**问题根因**：CSV 写入时机太晚（仅在所有博主完成后），中断时数据全部丢失。

### 1.3 主要使用场景

- 用户以关键词搜索大量博主（数百个），Stage1 耗时数小时
- 中途因各种原因中断（Ctrl+C、网络异常、系统重启等）
- 重新启动程序，选择断点续爬，期望之前已完成的博主数据不丢失

## 2. 目标 (Goals)

**核心目标**：CSV 文件是核心产出物，断点续爬的依据是 CSV 而不是 progress.json 中的 done_authors。

具体而言：
1. 每个博主完成后立即将数据追加写入 CSV 文件（实时落盘）
2. 中断后恢复时，从 CSV 文件读取已完成博主列表，作为跳过的**唯一依据**
3. 最终 CSV 包含所有博主数据，无论是否经历过中断

### 2.1 非目标 (Non-Goals)

- **不做粉丝数排序**：不按粉丝量倒序排序优先处理大博主（侵入较大，且平台默认排序已有播放量优先规则）
- **不改 pool.Run 接口**：不需要改造 Worker Pool 为流式回调模式

## 3. 需求细化 (Requirements)

### 3.1 功能性需求

1. **每个博主完成后立即追加写入 CSV**：实时落盘，确保中断时已完成博主的数据不丢失
2. **恢复时从 CSV 读取已完成博主列表**：作为跳过的唯一依据，判断"哪些博主已完成"
3. **progress.json 中记录当前 CSV 文件路径**：恢复时用于定位 CSV 文件（保留时间戳文件名）
4. **废弃 done_authors 字段**：不再在 progress.json 中记录已完成博主 map

### 3.2 非功能性需求

1. **并发安全**：当 concurrency > 1 时，多个 goroutine 可能同时完成博主处理，需要用锁串行化 CSV 追加写入（author 文件最多几千行，锁开销可忽略）

## 4. 设计方案 (Design)

### 4.1 方案概览

**一句话**：引入 `AuthorCSVWriter` 组件，持有文件句柄和互斥锁，在 `RunStage1` 的 worker 回调中每完成一个博主就追加写入 CSV；恢复时从 CSV 解析已完成博主 ID 作为跳过依据。废弃 `DoneAuthors` 字段和相关方法。

#### 模块划分与改动范围

| 模块 | 文件 | 改动类型 | 说明 |
|------|------|---------|------|
| CSV 导出 | `src/export/export.go` | **新增** | 新增 `AuthorCSVWriter` 结构体（`*os.File` + `*csv.Writer` + `sync.Mutex`），提供 `WriteRow(Author)` 追加写入和 `Close()` 方法；新增 `ReadCompletedAuthors(csvPath)` 从 CSV 读取已完成博主 ID 列表 |
| 进度管理 | `src/progress/progress.go` | **修改** | 新增 `AuthorCSVPath string` 字段；**删除** `DoneAuthors` 字段、`MarkDone` 方法、`PendingAuthors` 方法 |
| Stage1 编排 | `src/crawler.go` | **修改** | `RunStage1` 改为：先创建 `AuthorCSVWriter` → worker 回调中每完成一个博主调用 `WriteRow` → 结束后 `Close()`；`Stage1Config` 调整注入的函数签名；`ProgressTracker` 接口删除 `MarkDone` |
| 入口 | `cmd/crawler/main.go` | **修改** | 恢复逻辑：从 `prog.AuthorCSVPath` 定位 CSV → 调用 `ReadCompletedAuthors` 获取已完成 ID → 过滤 mids |

#### 数据流（改造后）

```
首次运行:
  ├─ NewAuthorCSVWriter(outputDir, platform, keyword) → 创建新文件 + 写 BOM + header
  ├─ progress.SetAuthorCSVPath(csvPath) → 记录 CSV 路径到 progress.json
  ├─ pool.Run(workers)
  │    └─ 每个 worker:
  │         ├─ processOneAuthor() → 获取博主数据
  │         └─ csvWriter.WriteRow(author) → 🔒 加锁追加写入 CSV（实时落盘）
  ├─ pool.Run 返回 results
  └─ csvWriter.Close()

恢复时（main.go 侧）:
  ├─ progress.AuthorCSVPath → 定位已有 CSV 文件
  ├─ ReadCompletedAuthors(csvPath) → 解析已完成博主 ID 集合
  ├─ 过滤 mids（排除已完成的）→ pending mids
  └─ 传入 RunStage1（cfg.ExistingCSVPath = csvPath）

恢复时（RunStage1 侧）:
  ├─ OpenAuthorCSVWriter(existingCSVPath) → 打开已有文件（追加模式，不写 header）
  ├─ pool.Run(workers) → 仅处理 pending mids
  │    └─ 每个 worker: processOneAuthor → csvWriter.WriteRow（追加到同一文件）
  └─ csvWriter.Close() → 最终一个 CSV 文件包含所有博主数据
```

#### 关键 trade-off

| 决策 | 选择 | 牺牲 | 理由 |
|------|------|------|------|
| CSV 写入时机 | 每个博主完成后立即写入 | 写入频率增加（但 author 数据量小，可忽略） | 核心目标：数据不丢失 |
| 并发控制 | `sync.Mutex` 串行化写入 | 微小的锁竞争 | 最多几千行，锁开销远小于网络 IO |
| `DoneAuthors` 废弃方式 | **直接删除字段和相关方法** | 旧 progress 文件不兼容（需重新开始） | 用户明确不需要兼容，保持代码干净 |
| `pool.Run` 接口 | 不改 | worker 内部需要持有 `CSVWriter` 引用 | spec 非目标，且通过闭包捕获即可 |

### 4.2 组件设计

#### 4.2.1 核心模块设计

##### 1. `AuthorCSVWriter`（新增，`src/export/export.go`）

**职责**：持有 CSV 文件句柄，提供并发安全的逐行追加写入能力。

```go
type AuthorCSVWriter struct {
    f    *os.File
    w    *csv.Writer
    mu   sync.Mutex
    path string  // 文件绝对路径，供 FilePath() 返回
}
```

**两种构造方式**：
- `NewAuthorCSVWriter(outputDir, platform, keyword)`：首次运行，创建新文件 + 写 BOM + 写 header
- `OpenAuthorCSVWriter(existingPath)`：恢复运行，以 `O_APPEND|O_WRONLY` 打开已有文件，**不写 header**

设计决策：
- 不持有 `platform`/`keyword`：仅在创建时用于生成文件名
- `Flush()` 在每次 `WriteRow` 内调用：确保数据实时落盘
- `Close()` 幂等：多次调用不 panic

##### 2. `ReadCompletedAuthors`（新增，`src/export/export.go`）

**职责**：从已有 CSV 文件中解析出已完成博主的 ID 集合。

CSV header 新增 `ID` 列（第二列），`ReadCompletedAuthors` 读取 index=1 的列作为博主 ID。
用 `ID`（平台 mid）而非"博主名字"匹配，因为名字可能重复或变更。

##### 3. `Progress` 结构体改造（`src/progress/progress.go`）

- **删除** `DoneAuthors map[string]bool` 字段
- **删除** `MarkDone` 方法
- **删除** `PendingAuthors` 方法
- **删除** `Load` 中 `DoneAuthors` 的初始化逻辑
- **新增** `AuthorCSVPath string` 字段（JSON tag: `author_csv_path`）
- **新增** `SetAuthorCSVPath(outputDir, csvPath string) error` 方法

##### 4. `ProgressTracker` 接口改造（`src/crawler.go`）

- **删除** `MarkDone(outputDir string, mid string) error`
- **新增** `SetAuthorCSVPath(outputDir string, csvPath string) error`

##### 5. `Stage1Config` 改造（`src/crawler.go`）

- **删除** `WriteAuthorCSV` 函数字段
- **新增** `NewAuthorCSVWriter func(outputDir, platform, keyword string) (*export.AuthorCSVWriter, error)` 函数字段
- **新增** `OpenAuthorCSVWriter func(existingPath string) (*export.AuthorCSVWriter, error)` 函数字段
- **新增** `ExistingCSVPath string` 字段（恢复时由 main.go 从 progress 中读取并传入，为空表示首次运行）

通过依赖注入保持可测试性。

##### 6. `RunStage1` 改造（`src/crawler.go`）

核心逻辑变更：

```go
func RunStage1(ctx, ac, mids, cfg) error {
    // 1. 创建或打开 CSVWriter
    var csvWriter *export.AuthorCSVWriter
    if cfg.ExistingCSVPath != "" {
        // 恢复场景：打开已有 CSV，追加写入（不写 header）
        csvWriter, err = cfg.OpenAuthorCSVWriter(cfg.ExistingCSVPath)
    } else {
        // 首次运行：创建新文件 + 写 header
        csvWriter, err = cfg.NewAuthorCSVWriter(cfg.OutputDir, cfg.Platform, cfg.Keyword)
    }
    defer csvWriter.Close()
    
    // 2. 记录 CSV 路径到 progress（首次运行时写入，恢复时路径不变）
    cfg.Progress.SetAuthorCSVPath(cfg.OutputDir, csvWriter.FilePath())
    
    // 3. pool.Run — worker 闭包捕获 csvWriter
    results := cfg.PoolRun(ctx, cfg.Concurrency, mids,
        func(ctx, mid) (Author, error) {
            author, err := processOneAuthor(ctx, ac, mid, cfg)
            if err != nil { return Author{}, err }
            // 实时写入 CSV（替代原来的 MarkDone）
            csvWriter.WriteRow(author)
            return author, nil
        }, ...)
    
    // 4. 统计结果（仅日志，不再需要收集 authors 写 CSV）
    return nil
}
```

##### 7. `main.go` 恢复逻辑改造

```go
// 恢复时：从 CSV 过滤已完成博主（替代原来的 PendingAuthors）
var existingCSVPath string
if prog.Stage >= 1 && len(prog.AuthorMids) > 0 {
    mids = prog.AuthorMids  // 全量 mids
    if prog.AuthorCSVPath != "" {
        existingCSVPath = prog.AuthorCSVPath  // 传给 Stage1Config
        completedIDs, _ := export.ReadCompletedAuthors(prog.AuthorCSVPath)
        var pending []src.AuthorMid
        for _, mid := range mids {
            if !completedIDs[mid.ID] {
                pending = append(pending, mid)
            }
        }
        mids = pending
    }
}

// 构造 Stage1Config 时传入 existingCSVPath
cfg := Stage1Config{
    ExistingCSVPath: existingCSVPath,  // 恢复时非空，首次运行为空
    // ... 其他字段
}
```

#### 4.2.2 接口设计

##### `AuthorCSVWriter` 公开 API

```go
// NewAuthorCSVWriter 创建新 CSV 文件并写入 BOM + header 行。
// 用于首次运行。调用方必须在结束时调用 Close()。
func NewAuthorCSVWriter(outputDir, platform, keyword string) (*AuthorCSVWriter, error)

// OpenAuthorCSVWriter 打开已有 CSV 文件，以追加模式写入（不写 header）。
// 用于恢复运行。调用方必须在结束时调用 Close()。
func OpenAuthorCSVWriter(existingPath string) (*AuthorCSVWriter, error)

// WriteRow 追加一行博主数据到 CSV。并发安全，每次写入后立即 Flush。
func (w *AuthorCSVWriter) WriteRow(author Author) error

// FilePath 返回 CSV 文件的绝对路径。
func (w *AuthorCSVWriter) FilePath() string

// Close 刷新缓冲区并关闭文件句柄。幂等，多次调用安全。
func (w *AuthorCSVWriter) Close() error

// ReadCompletedAuthors 从 CSV 文件读取已完成博主 ID 集合。
// 文件不存在时返回空 map（不报错）。
func ReadCompletedAuthors(csvPath string) (map[string]bool, error)
```

##### `ProgressTracker` 接口（改造后）

```go
type ProgressTracker interface {
    CompletedPages() map[int]bool
    AddSearchPage(outputDir string, page int) error
    SetAuthorMids(outputDir string, mids []AuthorMid) error
    SetAuthorCSVPath(outputDir string, csvPath string) error  // 新增
    // MarkDone 已删除
}
```

#### 4.2.3 数据模型

##### CSV Schema 变更

现有 header（12 列）：
```
博主名字, 粉丝数, 视频数量, 视频平均播放量, 视频平均时长, 视频平均评论数, 视频平均点赞量, 地区, 语言, 视频_TOP1, 视频_TOP2, 视频_TOP3
```

改造后 header（13 列，新增 `ID` 列）：
```
博主名字, ID, 粉丝数, 视频数量, 视频平均播放量, 视频平均时长, 视频平均评论数, 视频平均点赞量, 地区, 语言, 视频_TOP1, 视频_TOP2, 视频_TOP3
```

##### `progress.json` Schema 变更

删除字段：`done_authors`
新增字段：`author_csv_path`（string，CSV 文件绝对路径）

#### 4.2.4 并发模型

```
  ┌─────────┐  ┌─────────┐  ┌─────────┐
  │ Worker1 │  │ Worker2 │  │ Worker3 │
  │ process │  │ process │  │ process │
  │ Author  │  │ Author  │  │ Author  │
  │    │    │  │    │    │  │    │    │
  │ WriteRow│  │ WriteRow│  │ WriteRow│
  └────┬────┘  └────┬────┘  └────┬────┘
       │            │            │
       ▼            ▼            ▼
  ┌──────────────────────────────────┐
  │     sync.Mutex（串行化写入）       │
  │  csv.Writer.Write → Flush → 落盘  │
  │       AuthorCSVWriter             │
  └──────────────────────────────────┘
```

- **共享状态**：`AuthorCSVWriter` 内部的 `*os.File` + `*csv.Writer`
- **同步机制**：`sync.Mutex`，锁粒度为单次 Write + Flush
- **死锁风险**：无（单锁，无嵌套）
- **性能影响**：可忽略（CSV 写入微秒级，worker 主要耗时在网络请求秒级）

#### 4.2.5 错误处理

| 失败场景 | 错误类型 | 处理方式 |
|----------|---------|---------|
| CSV 文件创建失败 | 不可重试 | `RunStage1` 直接返回 error，终止任务 |
| `WriteRow` 写入/Flush 失败 | 不可重试 | 打 WARN 日志，不中断 worker |
| `ReadCompletedAuthors` 解析失败 | 不可重试 | 返回 error，`main.go` 决定是否 fatal |
| CSV 文件不存在（恢复时） | 预期情况 | 返回空 map，视为无已完成博主 |
| `SetAuthorCSVPath` 保存失败 | 不可重试 | 打 WARN 日志（不影响当前运行） |

`WriteRow` 失败不中断 worker 的理由：数据仍在 `pool.Run` 的 results 中（当前运行内不丢失），磁盘 IO 失败通常是全局性的，中断单个 worker 无意义。
