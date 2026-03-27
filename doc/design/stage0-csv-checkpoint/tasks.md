# 实施任务清单

> 由 spec.md 生成
> 任务总数: 3
> 核心原则: 先建后迁——先新增 VideoCSVWriter 和 Progress 新字段，再改造 RunStage0/main.go

## 依赖关系总览

```
Task 1 (export: VideoCSVWriter + ReadVideoCSV + Video CSV header 新增 AuthorID 列)
  ↓
Task 2 (progress + crawler: 新增 VideoCSVPath，改造 ProgressTracker/Stage0Config/RunStage0)
  ↓
Task 3 (main.go: Stage0 恢复逻辑 + Stage0Config 注入)
```

## 变更影响概览

### 文件变更清单

| 文件 | 操作 | 涉及任务 | 说明 |
|------|------|---------|------|
| `src/export/export.go` | 修改 | Task 1 | 新增 VideoCSVWriter 结构体、New/Open/WriteRows/Close、ReadVideoCSV；修改 WriteVideoCSV header/row 新增 AuthorID 列 |
| `src/progress/progress.go` | 修改 | Task 2 | 新增 VideoCSVPath 字段和 SetVideoCSVPath 方法 |
| `src/crawler.go` | 修改 | Task 2 | 新增 VideoCSVRowWriter 接口，改造 ProgressTracker/Stage0Config/RunStage0 |
| `cmd/crawler/main.go` | 修改 | Task 3 | Stage0 恢复逻辑 + Stage0Config 注入新依赖 |

## 任务列表

### 任务 1: [x] 新增 VideoCSVWriter 和 ReadVideoCSV *(Completed)*
- 文件: `src/export/export.go`（修改）
- 依赖: 无
- spec 映射: 4.2.1 #1 VideoCSVWriter, 4.2.1 #2 ReadVideoCSV, 4.2.2 接口设计, 4.2.3 Video CSV Schema 变更
- 说明: 在 export.go 中新增 `VideoCSVWriter` 结构体及其方法（`NewVideoCSVWriter`、`OpenVideoCSVWriter`、`WriteRows`、`FilePath`、`Close`），新增 `ReadVideoCSV` 函数。同时修改现有 `WriteVideoCSV` 的 header 和 row 新增 AuthorID 列。提取共享的 `videoCSVHeader` 和 `videoToRow` 辅助函数（与 AuthorCSVWriter 的模式一致）。
- context:
  - `src/export/export.go` — 直接修改目标
  - `src/types.go:Video` — WriteRows 需要读取 Video 字段
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `go vet ./...` 无新警告
  - [ ] VideoCSVWriter 结构体包含 `*os.File` + `*csv.Writer` + `sync.Mutex` + `path`
  - [ ] NewVideoCSVWriter 创建文件、写 BOM、写 7 列 header（含 AuthorID）
  - [ ] OpenVideoCSVWriter 以 O_APPEND|O_WRONLY 打开已有文件，不写 header
  - [ ] WriteRows 加锁批量写入多行数据并 Flush
  - [ ] Close 幂等
  - [ ] ReadVideoCSV 从 CSV 读取全部视频数据，返回 `[]Video`，文件不存在返回空 slice
  - [ ] 现有 WriteVideoCSV 的 header/row 也新增 AuthorID 列
- 子任务:
  - [ ] 1.1: 提取 videoCSVHeader 和 videoToRow 辅助函数，修改 WriteVideoCSV 使用它们
  - [ ] 1.2: 定义 VideoCSVWriter 结构体
  - [ ] 1.3: 实现 NewVideoCSVWriter（创建文件 + BOM + header）
  - [ ] 1.4: 实现 OpenVideoCSVWriter（追加模式打开）
  - [ ] 1.5: 实现 WriteRows（加锁 + 批量 Write + Flush）
  - [ ] 1.6: 实现 FilePath 和 Close（幂等）
  - [ ] 1.7: 实现 ReadVideoCSV

### 任务 2: [x] 改造 Progress/ProgressTracker/Stage0Config/RunStage0 *(Completed)*
- 文件: `src/progress/progress.go`（修改）、`src/crawler.go`（修改）
- 依赖: Task 1
- spec 映射: 4.2.1 #3 Progress 扩展, 4.2.1 #4 ProgressTracker 扩展, 4.2.1 #5 Stage0Config 改造, 4.2.1 #6 RunStage0 改造
- 说明: (1) Progress 新增 VideoCSVPath 字段和 SetVideoCSVPath 方法。(2) ProgressTracker 接口新增 SetVideoCSVPath。(3) Stage0Config 删除 WriteVideoCSV，新增 NewVideoCSVWriter/OpenVideoCSVWriter/ReadVideoCSV/ExistingVideoCSVPath。(4) RunStage0 改为 VideoCSVWriter 模式：创建/打开 → worker 中 WriteRows → Close → ReadVideoCSV 去重。(5) 新增 VideoCSVRowWriter 接口（避免循环导入）。
- context:
  - `src/progress/progress.go` — 新增字段和方法
  - `src/crawler.go` — 改造 ProgressTracker、Stage0Config、RunStage0
  - `src/export/export.go:VideoCSVWriter` — RunStage0 使用的新组件
  - `cmd/crawler/main.go` — 调用方（Task 3 改造）
- 验收标准:
  - [ ] `go build ./...` 编译通过（注意：main.go 中 Stage0Config 构造需要同步更新以保持编译通过）
  - [ ] `go vet ./...` 无新警告
  - [ ] Progress 有 VideoCSVPath 字段和 SetVideoCSVPath 方法
  - [ ] ProgressTracker 接口有 SetVideoCSVPath
  - [ ] Stage0Config 无 WriteVideoCSV，有 NewVideoCSVWriter/OpenVideoCSVWriter/ReadVideoCSV/ExistingVideoCSVPath
  - [ ] RunStage0 中 worker 回调调用 csvWriter.WriteRows
  - [ ] RunStage0 结束后从 CSV 读取全部视频进行去重
- 子任务:
  - [ ] 2.1: Progress 新增 VideoCSVPath 字段和 SetVideoCSVPath 方法
  - [ ] 2.2: ProgressTracker 接口新增 SetVideoCSVPath
  - [ ] 2.3: 新增 VideoCSVRowWriter 接口
  - [ ] 2.4: Stage0Config 改造（删除 WriteVideoCSV，新增新字段）
  - [ ] 2.5: RunStage0 改造为 VideoCSVWriter 模式
  - [ ] 2.6: 更新 main.go 中 Stage0Config 构造（保持编译通过，ExistingVideoCSVPath 暂设为空）

### 任务 3: [x] 改造 main.go Stage0 恢复逻辑 *(Completed)*
- 文件: `cmd/crawler/main.go`（修改）
- 依赖: Task 1, 2
- spec 映射: 4.1 数据流（恢复时）
- 说明: Stage 0 恢复时，从 `prog.VideoCSVPath` 定位已有视频 CSV，将 `existingVideoCSVPath` 传入 Stage0Config。恢复时 RunStage0 内部会自动处理（打开已有 CSV 追加写入 + 最终从 CSV 读取全部视频去重）。
- context:
  - `cmd/crawler/main.go` — 直接修改目标
  - `src/progress/progress.go:Progress.VideoCSVPath` — 读取 CSV 路径
  - `src/crawler.go:Stage0Config.ExistingVideoCSVPath` — 传入恢复路径
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `go vet ./...` 无新警告
  - [ ] 恢复时 existingVideoCSVPath 正确传入 Stage0Config
- 子任务:
  - [ ] 3.1: 恢复逻辑：从 prog.VideoCSVPath 读取路径，传入 Stage0Config.ExistingVideoCSVPath

---

## Spec 覆盖映射

| Spec 章节 | 任务 | 说明 |
|-----------|------|------|
| 4.2.1 #1 VideoCSVWriter | Task 1 | 新增结构体和 New/Open/WriteRows/Close |
| 4.2.1 #2 ReadVideoCSV | Task 1 | 新增函数 |
| 4.2.1 #3 Progress 扩展 | Task 2 | 新增 VideoCSVPath |
| 4.2.1 #4 ProgressTracker 扩展 | Task 2 | 新增 SetVideoCSVPath |
| 4.2.1 #5 Stage0Config 改造 | Task 2 | 删除 WriteVideoCSV，新增新字段 |
| 4.2.1 #6 RunStage0 改造 | Task 2 | VideoCSVWriter 模式 |
| 4.2.2 接口设计 | Task 1, 2 | API 定义 |
| 4.2.3 Video CSV Schema | Task 1 | header 新增 AuthorID 列 |
| 4.2.3 progress.json Schema | Task 2 | 新增 video_csv_path |
| 4.2.4 并发模型 | Task 2 | sync.Mutex 在 WriteRows 中 |
| 4.2.5 错误处理 | Task 1, 2 | 各失败场景处理 |
| 4.1 数据流（恢复时） | Task 3 | main.go 恢复逻辑 |
