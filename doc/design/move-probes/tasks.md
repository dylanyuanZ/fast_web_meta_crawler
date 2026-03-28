# Tasks: 移动探针测试文件到独立 probe/ 目录

> Status: Draft
> Spec: N/A (simple refactoring)

## 背景

将 `src/platform/bilibili/` 下的 3 个探针测试文件移动到项目根目录下的 `probe/` 目录，改为独立 package `probe`。

## 依赖分析

探针文件引用了 bilibili 包的以下符号：
- **导出符号**（可直接通过 import 使用）：`BilibiliLoginChecker`, `NewAuthorCrawler`, `NewSearchCrawler`, `BiliBrowserAuthorCrawler`, `SearchData`
- **未导出符号**（需要先导出）：`ssrExtractJS`（search.go 中的 const）

## 任务列表

- [x] **Task 1**: 导出 `ssrExtractJS` — 将 `search.go` 中的 `ssrExtractJS` 重命名为 `SSRExtractJS`，同时更新 `search.go` 内部的引用
- [x] **Task 2**: 创建 `probe/` 目录，移动 3 个探针测试文件，修改 package 声明和 import 路径
  - `probe_test.go` → `probe/probe_test.go`
  - `probe_412_test.go` → `probe/probe_412_test.go`
  - `probe_stage1_test.go` → `probe/probe_stage1_test.go`
  - package 改为 `probe`
  - 添加 `bilibili "github.com/dylanyuanZ/fast_web_meta_crawler/src/platform/bilibili"` import
  - 所有 bilibili 包符号加上 `bilibili.` 前缀
  - 相对路径（`../../../conf/` 等）调整为 `../conf/` 等
- [x] **Task 3**: 删除原始文件，编译验证
