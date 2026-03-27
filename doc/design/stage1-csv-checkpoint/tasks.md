# 实施任务清单

> 由 spec.md 生成
> 任务总数: 4
> 核心原则: 先建后迁后删——先新增 AuthorCSVWriter 和 Progress 新字段，再迁移 RunStage1/main.go 到新逻辑，最后删除废弃代码

## 依赖关系总览

```
Task 1 (export: AuthorCSVWriter + ReadCompletedAuthors)
  ↓
Task 2 (progress: 新增 AuthorCSVPath，删除 DoneAuthors)  ← 依赖 Task 1（编译需要）
  ↓
Task 3 (crawler: 改造 ProgressTracker/Stage1Config/RunStage1)  ← 依赖 Task 1, 2
  ↓
Task 4 (main.go: 恢复逻辑改造 + 注入新依赖)  ← 依赖 Task 1, 2, 3
```

## 变更影响概览

### 文件变更清单

| 文件 | 操作 | 涉及任务 | 说明 |
|------|------|---------|------|
| `src/export/export.go` | 修改 | Task 1 | 新增 AuthorCSVWriter 结构体、New/Open 构造函数、WriteRow、ReadCompletedAuthors；修改 WriteAuthorCSV header 新增 ID 列 |
| `src/progress/progress.go` | 修改 | Task 2 | 新增 AuthorCSVPath 字段和 SetAuthorCSVPath 方法；删除 DoneAuthors 字段、MarkDone、PendingAuthors |
| `src/crawler.go` | 修改 | Task 3 | 改造 ProgressTracker 接口、Stage1Config、RunStage1 |
| `cmd/crawler/main.go` | 修改 | Task 4 | 改造恢复逻辑和 Stage1Config 构造 |

## 任务列表

### 任务 1: [x] 新增 AuthorCSVWriter 和 ReadCompletedAuthors *(Completed)*
- 文件: `src/export/export.go`（修改）
- 依赖: 无
- spec 映射: 4.2.1 #1 AuthorCSVWriter, 4.2.1 #2 ReadCompletedAuthors, 4.2.2 接口设计, 4.2.3 CSV Schema 变更
- 说明: 在 export.go 中新增 `AuthorCSVWriter` 结构体及其方法（`NewAuthorCSVWriter`、`OpenAuthorCSVWriter`、`WriteRow`、`FilePath`、`Close`），新增 `ReadCompletedAuthors` 函数。同时修改现有 `WriteAuthorCSV` 的 header 和 row 新增 ID 列（保持向前兼容，旧函数仍保留供后续任务迁移后再决定是否删除）。
- context:
  - `src/export/export.go` — 直接修改目标
  - `src/types.go:Author` — WriteRow 需要读取 Author.ID 字段
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `go vet ./...` 无新警告
  - [ ] AuthorCSVWriter 结构体包含 `*os.File` + `*csv.Writer` + `sync.Mutex` + `path`
  - [ ] NewAuthorCSVWriter 创建文件、写 BOM、写 13 列 header（含 ID）
  - [ ] OpenAuthorCSVWriter 以 O_APPEND|O_WRONLY 打开已有文件，不写 header
  - [ ] WriteRow 加锁写入一行数据并 Flush
  - [ ] Close 幂等
  - [ ] ReadCompletedAuthors 读取 CSV 第二列（index=1）作为 ID，文件不存在返回空 map
- 子任务:
  - [ ] 1.1: 定义 AuthorCSVWriter 结构体
  - [ ] 1.2: 实现 NewAuthorCSVWriter（创建文件 + BOM + header）
  - [ ] 1.3: 实现 OpenAuthorCSVWriter（追加模式打开）
  - [ ] 1.4: 实现 WriteRow（加锁 + Write + Flush）
  - [ ] 1.5: 实现 FilePath 和 Close（幂等）
  - [ ] 1.6: 实现 ReadCompletedAuthors
  - [ ] 1.7: 修改 WriteAuthorCSV 的 header 和 row 新增 ID 列

### 任务 2: [x] 改造 Progress 结构体 *(Completed)*
- 文件: `src/progress/progress.go`（修改）
- 依赖: Task 1（编译依赖：Task 3 会同时引用 export 和 progress，需要两者都就绪）
- spec 映射: 4.2.1 #3 Progress 结构体改造, 4.2.3 progress.json Schema 变更
- 说明: 删除 `DoneAuthors` 字段、`MarkDone` 方法、`PendingAuthors` 方法、`Load` 中 `DoneAuthors` 初始化逻辑、`NewProgress` 中 `DoneAuthors` 初始化。新增 `AuthorCSVPath string` 字段（JSON tag: `author_csv_path`）和 `SetAuthorCSVPath` 方法。
- context:
  - `src/progress/progress.go` — 直接修改目标
  - `src/crawler.go:ProgressTracker` — 接口定义（Task 3 改造）
  - `src/crawler.go:RunStage1()` — 调用 MarkDone 的地方（Task 3 改造）
  - `cmd/crawler/main.go` — 调用 PendingAuthors 的地方（Task 4 改造）
- 验收标准:
  - [ ] `go build ./...` 编译通过（注意：此时 crawler.go 的 ProgressTracker 接口仍有 MarkDone，需要在 progress.go 中保留 MarkDone 的桩实现，或者与 Task 3 同步改造。选择方案：保留 MarkDone 和 PendingAuthors 的空桩，在 Task 3 中一并删除接口定义）
  - [ ] `go vet ./...` 无新警告
  - [ ] Progress 结构体不再有 DoneAuthors 字段
  - [ ] 新增 AuthorCSVPath 字段和 SetAuthorCSVPath 方法
- 子任务:
  - [ ] 2.1: 删除 DoneAuthors 字段，修改 Load 和 NewProgress 中的初始化
  - [ ] 2.2: 新增 AuthorCSVPath 字段
  - [ ] 2.3: 新增 SetAuthorCSVPath 方法
  - [ ] 2.4: 将 MarkDone 改为空桩（`// Stub: will be removed in Task 3`），PendingAuthors 同理，确保编译通过

### 任务 3: [x] 改造 ProgressTracker 接口、Stage1Config 和 RunStage1 *(Completed)*
- 文件: `src/crawler.go`（修改）
- 依赖: Task 1, Task 2
- spec 映射: 4.2.1 #4 ProgressTracker 接口改造, 4.2.1 #5 Stage1Config 改造, 4.2.1 #6 RunStage1 改造, 4.2.4 并发模型, 4.2.5 错误处理
- 说明: (1) ProgressTracker 接口：删除 `MarkDone`，新增 `SetAuthorCSVPath`。(2) Stage1Config：删除 `WriteAuthorCSV` 字段，新增 `NewAuthorCSVWriter`、`OpenAuthorCSVWriter`、`ExistingCSVPath` 字段。(3) RunStage1：改为先创建/打开 CSVWriter → worker 回调中 WriteRow → 结束后 Close。同时删除 progress.go 中 Task 2 留下的 MarkDone/PendingAuthors 桩。
- context:
  - `src/crawler.go` — 直接修改目标
  - `src/export/export.go:AuthorCSVWriter` — RunStage1 使用的新组件
  - `src/progress/progress.go` — 删除桩方法
  - `cmd/crawler/main.go` — 调用方（Task 4 改造）
- 验收标准:
  - [ ] `go build ./...` 编译通过（注意：main.go 中仍引用旧字段，需要在 main.go 中添加临时桩或与 Task 4 协调。选择方案：Task 3 完成后 main.go 编译会失败，因此 Task 3 需要同时更新 main.go 中 Stage1Config 的构造以保持编译通过——但这会侵入 Task 4 的范围。**修正**：Task 3 和 Task 4 合并为一个编译单元处理，即 Task 3 同时修改 main.go 中 Stage1Config 的字段赋值，Task 4 专注于恢复逻辑改造）
  - [ ] `go vet ./...` 无新警告
  - [ ] ProgressTracker 接口无 MarkDone，有 SetAuthorCSVPath
  - [ ] Stage1Config 无 WriteAuthorCSV，有 NewAuthorCSVWriter/OpenAuthorCSVWriter/ExistingCSVPath
  - [ ] RunStage1 中 worker 回调调用 csvWriter.WriteRow 而非 MarkDone
  - [ ] progress.go 中 MarkDone 和 PendingAuthors 桩已删除
- 子任务:
  - [ ] 3.1: ProgressTracker 接口：删除 MarkDone，新增 SetAuthorCSVPath
  - [ ] 3.2: Stage1Config：删除 WriteAuthorCSV，新增 NewAuthorCSVWriter/OpenAuthorCSVWriter/ExistingCSVPath
  - [ ] 3.3: RunStage1：改造为 CSVWriter 模式（创建/打开 → WriteRow → Close）
  - [ ] 3.4: 更新 main.go 中 Stage1Config 构造（删除 WriteAuthorCSV 赋值，新增 NewAuthorCSVWriter/OpenAuthorCSVWriter 赋值，ExistingCSVPath 暂设为空字符串）
  - [ ] 3.5: 删除 progress.go 中 MarkDone 和 PendingAuthors 桩方法

### 任务 4: [x] 改造 main.go 恢复逻辑 *(Completed)*
- 文件: `cmd/crawler/main.go`（修改）
- 依赖: Task 1, 2, 3
- spec 映射: 4.2.1 #7 main.go 恢复逻辑改造, 4.1 数据流（恢复时）
- 说明: 恢复时从 `prog.AuthorCSVPath` 定位 CSV 文件，调用 `ReadCompletedAuthors` 获取已完成 ID 集合，过滤 mids，将 `existingCSVPath` 传入 Stage1Config。删除对 `PendingAuthors()` 的调用。
- context:
  - `cmd/crawler/main.go` — 直接修改目标
  - `src/export/export.go:ReadCompletedAuthors` — 恢复时调用
  - `src/progress/progress.go:Progress.AuthorCSVPath` — 读取 CSV 路径
  - `src/crawler.go:Stage1Config.ExistingCSVPath` — 传入恢复路径
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `go vet ./...` 无新警告
  - [ ] 恢复时从 CSV 读取已完成博主 ID（而非 PendingAuthors）
  - [ ] existingCSVPath 正确传入 Stage1Config
  - [ ] 不再调用 PendingAuthors()
- 子任务:
  - [ ] 4.1: 改造恢复逻辑：从 prog.AuthorCSVPath 读取 CSV 路径，调用 ReadCompletedAuthors 过滤 mids
  - [ ] 4.2: 将 existingCSVPath 传入 Stage1Config.ExistingCSVPath
  - [ ] 4.3: 删除对 PendingAuthors() 的调用

---

## Spec 覆盖映射

| Spec 章节 | 任务 | 说明 |
|-----------|------|------|
| 4.2.1 #1 AuthorCSVWriter | Task 1 | 新增结构体和 New/Open/WriteRow/Close |
| 4.2.1 #2 ReadCompletedAuthors | Task 1 | 新增函数 |
| 4.2.1 #3 Progress 改造 | Task 2 | 删除 DoneAuthors，新增 AuthorCSVPath |
| 4.2.1 #4 ProgressTracker 接口 | Task 3 | 删除 MarkDone，新增 SetAuthorCSVPath |
| 4.2.1 #5 Stage1Config 改造 | Task 3 | 删除 WriteAuthorCSV，新增新字段 |
| 4.2.1 #6 RunStage1 改造 | Task 3 | CSVWriter 模式 |
| 4.2.1 #7 main.go 恢复逻辑 | Task 4 | 从 CSV 恢复 |
| 4.2.2 接口设计 | Task 1, 3 | API 定义 |
| 4.2.3 CSV Schema 变更 | Task 1 | header 新增 ID 列 |
| 4.2.3 progress.json Schema | Task 2 | 增删字段 |
| 4.2.4 并发模型 | Task 3 | sync.Mutex 在 WriteRow 中 |
| 4.2.5 错误处理 | Task 1, 3 | 各失败场景处理 |
