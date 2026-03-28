# 全局日志模块

> Status: Quick Draft

## 1. 背景

当前项目日志分散：
- 大部分代码使用标准库 `log.Printf` / `log.Fatalf` 输出到控制台（stderr）
- `src/browser/logger.go` 有独立的 `debugLog` 写入 `log/` 目录文件
- 没有统一的日志管理，不便于排查问题

## 2. 目标

实现全局 log 模块，所有日志统一输出到 `log/` 目录下的文件，每次运行生成新的日志文件。

## 3. 需求

- 所有日志输出到 `log/` 目录下的文件
- 每次运行生成新的日志文件（带时间戳）
- 同时保留控制台输出（方便实时查看）
- 替换所有现有的 `log.Printf` / `log.Fatalf` 调用
- 合并现有 `browser/logger.go` 的 debug log 功能

## 4. 设计方案

### 4.1 全局 log 模块

创建 `src/log/log.go`：
- `Init(logDir string) error`：创建日志目录，打开带时间戳的日志文件，设置 `log.SetOutput(io.MultiWriter(os.Stderr, file))`
- `Close()`：关闭日志文件
- 文件命名：`crawler_<timestamp>.log`
- 使用标准库 `log` 的全局 logger，设置 output 为 MultiWriter，这样所有使用 `log.Printf` 的代码无需修改 import

### 4.2 browser debug log 整合

- 保留 `browser/logger.go` 的 debug log 功能（写入独立的 debug 文件）
- debug log 继续使用独立的 logger 实例，不走全局 log

### 4.3 入口初始化

- `cmd/crawler/main.go` 和 `cmd/probe/main.go` 在启动时调用 `Init("log")`
- defer `Close()`
