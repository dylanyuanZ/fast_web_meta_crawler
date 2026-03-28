# 实施任务清单

> 由 spec.md 生成
> 任务总数: 3
> 核心原则: 先建全局 log 模块，再在入口初始化，最后替换所有调用点（机械重复合并为一个任务）

## 依赖关系总览

```
Task 1 (创建全局 log 模块 src/log/log.go)
  ↓
Task 2 (入口初始化：cmd/crawler/main.go, cmd/probe/main.go)  ← 依赖 Task 1
  ↓
Task 3 (无额外改动，Task 2 已覆盖所有调用点)
```

## 变更影响概览

### 文件变更清单

| 文件 | 操作 | 涉及任务 | 说明 |
|------|------|---------|------|
| `src/log/log.go` | 新建 | Task 1 | 全局日志模块 |
| `cmd/crawler/main.go` | 修改 | Task 2 | 初始化全局 log |
| `cmd/probe/main.go` | 修改 | Task 2 | 初始化全局 log |

## 任务列表

### 任务 1: [x] 创建全局 log 模块
- 文件: `src/log/log.go`（新建）
- 依赖: 无
- spec 映射: spec 章节 4.1
- 说明: 创建 `src/log/log.go`，提供 `Init(logDir string) error` 和 `Close()` 函数。Init 创建 log 目录，打开带时间戳的日志文件（`crawler_20060102_150405.log`），使用 `io.MultiWriter(os.Stderr, file)` 设置 `log.SetOutput`，使所有标准库 log 调用同时输出到控制台和文件。
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] Init 创建 `log/` 目录和日志文件
  - [ ] Close 正确关闭文件句柄
- 子任务:
  - [ ] 1.1: 创建 `src/log/log.go`，实现 Init 和 Close
  - [ ] 1.2: 确保 package 名不与标准库 `log` 冲突（使用 `applog` 作为 package 名）

### 任务 2: [x] 入口初始化全局 log
- 文件: `cmd/crawler/main.go`（修改）, `cmd/probe/main.go`（修改）
- 依赖: Task 1
- spec 映射: spec 章节 4.3
- 说明: 在两个入口的 main 函数最早期调用 `applog.Init("log")`，defer `applog.Close()`。由于 Init 设置的是标准库 `log` 的全局 output，所有现有的 `log.Printf` / `log.Fatalf` 调用无需修改，自动生效。
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] 运行 crawler 后 `log/` 目录下生成新的日志文件
  - [ ] 日志同时输出到控制台和文件
- 子任务:
  - [ ] 2.1: 修改 `cmd/crawler/main.go`，在 flag.Parse() 后、config.Load() 前调用 Init
  - [ ] 2.2: 修改 `cmd/probe/main.go`，在 config.Load() 前调用 Init

## Spec 覆盖映射

| Spec 章节 | 任务 | 说明 |
|-----------|------|------|
| 4.1 全局 log 模块 | Task 1 | 创建模块 |
| 4.2 browser debug log | 无需改动 | 保持现有独立 debug logger |
| 4.3 入口初始化 | Task 2 | 两个入口初始化 |
