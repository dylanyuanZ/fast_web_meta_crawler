# 实施任务清单

> 由 spec.md 生成
> 任务总数: 3
> 核心原则: 先改配置结构体，再实现搜索排序，最后更新配置打印和示例文件

## 依赖关系总览

```
Task 1 (配置结构体: SortBy → SearchPageSortBy + AuthorPageSortBy)
  ↓
Task 2 (搜索排序: buildSPParam 支持 SearchPageSortBy)  ← 依赖 Task 1
  ↓
Task 3 (配置打印 + 示例文件 + FetchAllAuthorVideos 日志)  ← 依赖 Task 1
```

## 变更影响概览

### 文件变更清单

| 文件 | 操作 | 涉及任务 | 说明 |
|------|------|---------|------|
| `src/config/config.go` | 修改 | Task 1 | SortBy → SearchPageSortBy + AuthorPageSortBy |
| `src/platform/youtube/search.go` | 修改 | Task 2 | buildSPParam 支持排序 sp 参数 |
| `cmd/crawler/main.go` | 修改 | Task 3 | 更新配置打印 |
| `conf/config.yaml.example` | 修改 | Task 3 | 更新示例配置 |
| `conf/config.yaml` | 修改 | Task 3 | 更新实际配置 |
| `src/platform/youtube/author.go` | 修改 | Task 3 | FetchAllAuthorVideos 日志打印排序配置 |

## 任务列表

### 任务 1: [x] 配置结构体变更 — SortBy 拆分为 SearchPageSortBy + AuthorPageSortBy

- 文件: `src/config/config.go`（修改）
- 依赖: 无
- spec 映射: spec 章节 4.1
- 说明: 将 YouTubeConfig 中的 `SortBy string` 字段替换为 `SearchPageSortBy string` 和 `AuthorPageSortBy string` 两个独立字段
- 验收标准: `go build ./...` 编译通过
- 子任务:
  - [x] 1.1: 删除 `SortBy` 字段，新增 `SearchPageSortBy` (yaml: `search_page_sort_by`) 和 `AuthorPageSortBy` (yaml: `author_page_sort_by`)
  - [x] 1.2: 更新注释说明各自的可选值

### 任务 2: [x] 搜索排序实现 — buildSPParam 支持 SearchPageSortBy

- 文件: `src/platform/youtube/search.go`（修改）
- 依赖: Task 1
- spec 映射: spec 章节 3.1, 4.2
- 说明: 在 buildSPParam 函数中加入排序逻辑。当 SearchPageSortBy 为非空且非 "relevance" 时，生成对应的排序 sp 参数。排序 sp 值：popularity=`CAM%3D`。当同时有 filter 和 sort 时，排序优先（因为 YouTube 的 sp 参数是 protobuf 编码，简单组合不可行，需要使用预计算的组合值或排序优先策略）
- context:
  - `src/platform/youtube/search.go:buildSPParam()` — 直接修改目标
  - `src/platform/youtube/search.go:buildSearchURL()` — 上游调用方
  - `src/config/config.go:YouTubeConfig` — 数据来源
- 验收标准:
  - [x] `go build ./...` 编译通过
  - [x] 配置 `search_page_sort_by: "popularity"` 后，搜索 URL 包含 `sp=CAM%3D`
  - [x] 不配置或配置 "relevance" 时，行为与之前一致
- 子任务:
  - [x] 2.1: 在 buildSPParam 中增加排序逻辑，当有排序配置时优先使用排序 sp 值
  - [x] 2.2: 处理排序与 filter 的组合（排序优先策略：当同时配置了排序和 filter 时，使用排序的 sp 值）

### 任务 3: [x] 配置打印、示例文件更新、FetchAllAuthorVideos 日志

- 文件: `cmd/crawler/main.go`（修改）, `conf/config.yaml.example`（修改）, `conf/config.yaml`（修改）, `src/platform/youtube/author.go`（修改）
- 依赖: Task 1
- spec 映射: spec 章节 3.3, 4.3
- 说明: 更新 main.go 中 YouTube 配置打印，将 `sort_by` 替换为 `search_page_sort_by` 和 `author_page_sort_by`；更新配置示例文件；在 FetchAllAuthorVideos 的日志中打印 author_page_sort_by 配置值
- 验收标准:
  - [x] `go build ./...` 编译通过
  - [x] 启动日志中打印 `search_page_sort_by` 和 `author_page_sort_by` 的值
  - [x] config.yaml.example 和 config.yaml 中的注释和字段名正确
- 子任务:
  - [x] 3.1: 更新 main.go 中 YouTube 配置打印
  - [x] 3.2: 更新 conf/config.yaml.example 中的 sort_by 为两个新字段
  - [x] 3.3: 更新 conf/config.yaml 中的 sort_by 为两个新字段
  - [x] 3.4: 更新 FetchAllAuthorVideos 日志，打印 author_page_sort_by 配置

---

## Spec 覆盖映射

| Spec 章节 | 任务 | 说明 |
|-----------|------|------|
| 3.1 搜索页面排序 | Task 1, 2 | 配置字段 + sp 参数实现 |
| 3.2 作者视频页面排序 | Task 1, 3 | 配置字段 + 日志预留 |
| 3.3 配置打印 | Task 3 | 启动日志打印 |
| 4.1 配置结构体 | Task 1 | 字段拆分 |
| 4.2 搜索排序实现 | Task 2 | buildSPParam 逻辑 |
| 4.3 作者视频页面排序 | Task 3 | FetchAllAuthorVideos 日志 |
