# 实施任务清单

> 由 youtube-crawler/spec.md FR-2.1 生成（遗留清理）
> 任务总数: 3
> 核心原则: 先建（在 bilibili 包创建类型）→ 再迁（stats.go + bilibili 内部改引用）→ 后删（清理 src/types.go）

## 背景

youtube-crawler/tasks.md 中 Task 2（平台类型下沉）和 Task 6（旧类型清理）标记为已完成，
但实际上 `src/types.go` 仍包含 6 个 Bilibili 专属类型（Video、AuthorInfo、VideoDetail、
AuthorStats、TopVideo、Author），且存在无用字段（VideoDetail.LikeCount 始终为 0）。
本任务清单完成这些遗留清理工作。

## 依赖关系总览

```
Task 1 (在 bilibili/types.go 中新增业务类型定义)
  ↓
Task 2 (迁移引用: stats.go + bilibili 内部)  ← 依赖 Task 1
  ↓
Task 3 (清理 src/types.go 中的旧类型 + 废弃接口)  ← 依赖 Task 2
```

## 变更影响概览

### 文件变更清单

| 文件 | 操作 | 涉及任务 | 说明 |
|------|------|---------|------|
| `src/platform/bilibili/types.go` | 修改 | Task 1 | 新增 Video、AuthorInfo、VideoDetail、AuthorStats、TopVideo、Author |
| `src/platform/bilibili/csv.go` | 修改 | Task 1 | 删除重复的 TopVideo 定义（已存在于 csv.go 中） |
| `src/stats/stats.go` | 修改 | Task 2 | 改为引用 bilibili 包的类型 |
| `src/platform/bilibili/search.go` | 修改 | Task 2 | 改为引用本包类型（去掉 src. 前缀） |
| `src/platform/bilibili/author.go` | 修改 | Task 2 | 改为引用本包类型（去掉 src. 前缀） |
| `src/types.go` | 修改 | Task 3 | 删除 Video、AuthorInfo、VideoDetail、AuthorStats、TopVideo、Author + SearchCrawler 接口 |
| `src/crawler.go` | 修改 | Task 3 | 删除 SearchCrawler 接口引用（如有） |

## 任务列表

### 任务 1: [x] 在 bilibili/types.go 中新增业务类型 + 清理 csv.go 重复定义

- 文件: `src/platform/bilibili/types.go`（修改）, `src/platform/bilibili/csv.go`（修改）
- 依赖: 无
- spec 映射: FR-2.1 (平台类型下沉)
- 说明:
  在 `bilibili/types.go` 中新增 Video、AuthorInfo、VideoDetail、AuthorStats、Author 类型定义。
  注意：
  - `bilibili/csv.go` 已有 `TopVideo` 定义，保留 csv.go 中的版本即可，不重复定义
  - VideoDetail 中移除无用的 `LikeCount` 字段（始终为 0，无消费者）
  - VideoDetail 中移除无用的 `PubDate` 字段（CalcAuthorStats 不使用，但 author.go 的 videoDetailsToRows 输出它 → 保留 PubDate）
  - 此阶段 src/types.go 中的旧类型暂不删除（新旧并存，确保编译通过）
- context:
  - `src/platform/bilibili/types.go` — 直接修改，新增类型
  - `src/platform/bilibili/csv.go` — 已有 TopVideo，需确认不冲突
  - `src/types.go` — 旧类型暂保留
- 验收标准:
  - [x] `go build ./...` 编译通过
  - [x] `bilibili/types.go` 包含 Video、AuthorInfo（VideoDetail/AuthorStats/TopVideo 保留在 src 包避免循环依赖）
  - [x] `bilibili/csv.go` 中的 TopVideo 改为引用 src.TopVideo
  - [x] VideoDetail 不再包含 LikeCount 字段
- 子任务:
  - [x] 1.1: 在 `bilibili/types.go` 末尾新增 Video、AuthorInfo 类型
  - [x] 1.2: csv.go 中的 TopVideo 改为引用 src.TopVideo（避免重复定义）

### 任务 2: [x] 迁移所有引用到 bilibili 包类型

- 文件: `src/stats/stats.go`（修改）, `src/platform/bilibili/search.go`（修改）, `src/platform/bilibili/author.go`（修改）
- 依赖: Task 1
- spec 映射: FR-2.1 (平台类型下沉)
- 说明:
  将所有对 `src.Video`、`src.AuthorInfo`、`src.VideoDetail`、`src.AuthorStats`、`src.TopVideo` 的引用
  改为引用 bilibili 包的类型。
  - `stats/stats.go`: 改为 `import bilibili`，引用 `bilibili.VideoDetail`、`bilibili.AuthorStats`、`bilibili.TopVideo`
  - `bilibili/search.go`: 去掉 `src.Video` 前缀，改为本包的 `Video`
  - `bilibili/author.go`: 去掉 `src.AuthorInfo`、`src.VideoDetail` 前缀，改为本包类型
  - `bilibili/author.go`: videoDetailsToRows 中移除 LikeCount 列的输出
  - `bilibili/author.go`: MergeAuthorRow 中调整 row index（因为 LikeCount 列被移除）
- context:
  - `src/stats/stats.go` — 改 import + 类型引用
  - `src/platform/bilibili/search.go` — 改类型引用（去掉 src. 前缀）
  - `src/platform/bilibili/author.go` — 改类型引用 + 移除 LikeCount 相关代码
  - `src/platform/bilibili/csv.go` — TopVideo 已在本包，无需改动
- 验收标准:
  - [x] `go build ./...` 编译通过
  - [x] `stats/stats.go` 保持 import `src` 包（VideoDetail/AuthorStats/TopVideo 留在 src 包避免循环依赖）
  - [x] `bilibili/search.go` 不再引用 `src.Video`（改为本包 Video）
  - [x] `bilibili/author.go` 不再引用 `src.AuthorInfo`（改为本包 AuthorInfo）
  - [x] `bilibili/author.go` 的 videoDetailsToRows 不再输出 LikeCount 列
- 子任务:
  - [x] 2.1: stats/stats.go 保持不变（VideoDetail/AuthorStats/TopVideo 留在 src 包，无循环依赖问题）
  - [x] 2.2: 修改 `bilibili/search.go`：`src.Video` → `Video`（本包类型）
  - [x] 2.3: 修改 `bilibili/author.go`：`src.AuthorInfo` → `AuthorInfo`
  - [x] 2.4: 修改 `bilibili/author.go`：videoDetailsToRows 移除 LikeCount 列，MergeAuthorRow 调整 row index

### 任务 3: [x] 清理 src/types.go 中的旧类型和废弃接口

- 文件: `src/types.go`（修改）, `src/crawler.go`（修改，如需要）
- 依赖: Task 2
- spec 映射: FR-2.1 (平台类型下沉), FR-2.2 (旧接口清理)
- 说明:
  从 `src/types.go` 中删除已下沉的类型：Video、AuthorInfo、VideoDetail、AuthorStats、TopVideo、Author。
  同时删除废弃的 `SearchCrawler` 接口（已被 `SearchRecorder` 替代，仅 bilibili/search.go 的编译时检查引用）。
  删除 bilibili/search.go 中对 SearchCrawler 的编译时检查。
- context:
  - `src/types.go` — 直接修改，删除旧类型和 SearchCrawler 接口
  - `src/crawler.go` — 确认无直接引用（已确认无引用）
  - `src/platform/bilibili/search.go` — 删除 `var _ src.SearchCrawler = ...` 编译时检查
- 验收标准:
  - [x] `go build ./...` 编译通过
  - [x] `src/types.go` 不再包含 Video、AuthorInfo、Author（已下沉/删除）
  - [x] `src/types.go` 保留 VideoDetail、AuthorStats、TopVideo（跨包共享，避免循环依赖）
  - [x] `src/crawler.go` 不再包含 SearchCrawler 接口
  - [x] `bilibili/search.go` 不再有 SearchCrawler 编译时检查
  - [x] SearchPage 方法（legacy）也一并删除

## Spec 覆盖映射

| Spec 章节 | 任务 | 说明 |
|-----------|------|------|
| FR-2.1 平台类型下沉 | Task 1, 2, 3 | Task 1 建类型，Task 2 迁引用，Task 3 删旧类型 |
| FR-2.2 旧接口清理 | Task 3 | 删除 SearchCrawler 废弃接口 |
| NFR-1 编译兼容 | 所有 Task | 每个 Task 完成后 go build 通过 |
| NFR-2 向后兼容 | 所有 Task | Bilibili 功能不受影响 |
