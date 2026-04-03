# 实施任务清单

> 由 spec.md 生成
> 任务总数: 8
> 核心原则: 先建后迁后删——先创建通用 CSV Writer 和新接口，再迁移 Bilibili 到新接口，最后清理旧代码；YouTube 实现在基础设施就绪后并行推进

## 依赖关系总览

```
Task 1 (通用 CSVWriter + CSVRowWriter 接口)
  ↓
Task 2 (平台类型下沉: Bilibili types.go)  ← 依赖 Task 1
  ↓
Task 3 (ReadVideoCSV 返回 []AuthorMid + Bilibili CSV adapter 适配)  ← 依赖 Task 1, 2
  ↓
Task 4 (SearchRecorder 接口 + Bilibili 实现)  ← 依赖 Task 1, 2, 3
  ↓
Task 5 (编排层 RunStage0/1/2 适配 + AuthorCrawler 接口改造)  ← 依赖 Task 3, 4
  ↓
Task 6 (main.go 适配 + 旧接口清理)  ← 依赖 Task 5
  ↓
Task 7 (配置重构: 分阶段 + 分平台)  ← 依赖 Task 6
  ↓
Task 8 (YouTube 爬虫实现: types + csv + search + author)  ← 依赖 Task 7
```

## 变更影响概览

### 文件变更清单

| 文件 | 操作 | 涉及任务 | 说明 |
|------|------|---------|------|
| `src/export/export.go` | 修改 | Task 1, 3 | CSV Writer 通用化 + ReadVideoCSV 改返回值 |
| `src/types.go` | 修改 | Task 1, 2, 4, 5 | 删除平台类型，新增通用接口 |
| `src/platform/bilibili/types.go` | 新建 | Task 2 | 承接从 types.go 移出的 Bilibili 类型 |
| `src/platform/bilibili/csv.go` | 修改 | Task 3 | 适配通用 CSVRowWriter |
| `src/platform/bilibili/search.go` | 修改 | Task 4 | 实现 SearchRecorder 接口 |
| `src/platform/bilibili/author.go` | 修改 | Task 5 | FetchAuthorInfo 返回 []string |
| `src/crawler.go` | 修改 | Task 5 | RunStage0/1/2 适配通用 CSV |
| `src/stats/stats.go` | 修改 | Task 2 | 改用 bilibili 包的类型 |
| `cmd/crawler/main.go` | 修改 | Task 6 | 适配新接口和新配置 |
| `src/config/config.go` | 修改 | Task 7 | 配置结构重构 |
| `conf/config.yaml` | 修改 | Task 7 | 配置文件结构重构 |
| `src/platform/youtube/types.go` | 新建 | Task 8 | YouTube 平台类型 |
| `src/platform/youtube/csv.go` | 新建 | Task 8 | YouTube CSV 转换 |
| `src/platform/youtube/search.go` | 新建 | Task 8 | YouTube SearchRecorder 实现 |
| `src/platform/youtube/author.go` | 新建 | Task 8 | YouTube AuthorCrawler 实现 |
| `src/test/crawler_test.go` | 修改 | Task 5, 6 | 适配新接口 |

## 任务列表

### 任务 1: [x] 通用 CSVWriter + CSVRowWriter 接口

- 文件: `src/export/export.go`（修改）, `src/types.go`（修改）
- 依赖: 无
- spec 映射: FR-2.2 (CSV Writer 通用化)
- 说明:
  将 `export.go` 中的 `VideoCSVWriter` 和 `AuthorCSVWriter` 合并为一个通用的 `CSVWriter`，只操作 `[]string`。同时在 `types.go` 中新增 `CSVRowWriter` 接口定义（合并原有 `VideoCSVRowWriter` + `AuthorCSVRowWriter`）。**旧的 `VideoCSVWriter`/`AuthorCSVWriter` 暂时保留**（后续任务迁移完成后再删除），新旧并存确保编译通过。
- context:
  - `src/export/export.go` — 直接修改目标，新增 `CSVWriter` struct + `NewCSVWriter`/`OpenCSVWriter` 函数
  - `src/types.go` — 新增 `CSVRowWriter` 接口定义
  - `src/crawler.go:RunStage0()` — 下游消费方（Task 5 迁移）
  - `cmd/crawler/main.go` — 下游消费方（Task 6 迁移）
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `CSVWriter` 支持 `WriteRow([]string)`、`WriteRows([][]string)`、`FilePath()`、`Close()`
  - [ ] `CSVWriter` 实现 `CSVRowWriter` 接口（编译时检查）
  - [ ] 旧的 `VideoCSVWriter`/`AuthorCSVWriter` 仍然存在且可编译
- 子任务:
  - [ ] 1.1: 在 `src/types.go` 中新增 `CSVRowWriter` 接口定义
  - [ ] 1.2: 在 `src/export/export.go` 中新增 `CSVWriter` struct（通用，只操作 `[]string`）
  - [ ] 1.3: 实现 `NewCSVWriter(outputDir, platform, keyword, fileType string, header []string)` 函数
  - [ ] 1.4: 实现 `OpenCSVWriter(existingPath string)` 函数
  - [ ] 1.5: 添加编译时接口检查 `var _ src.CSVRowWriter = (*CSVWriter)(nil)`

### 任务 2: [x] 平台类型下沉 — Bilibili types.go (策略调整：保留 src.Video 供 bilibili/search.go 使用，stats.go 保持不变)

- 文件: `src/platform/bilibili/types.go`（新建）, `src/types.go`（修改）, `src/stats/stats.go`（修改）
- 依赖: Task 1
- spec 映射: FR-2.1 (平台类型下沉)
- 说明:
  将 `src/types.go` 中的 `Video`、`AuthorInfo`、`VideoDetail`、`AuthorStats`、`TopVideo`、`Author` 移到 `src/platform/bilibili/types.go`。`src/types.go` 只保留通用类型（`PageInfo`、`AuthorMid`、`PoolResult`）和接口。`stats.go` 改为引用 bilibili 包的类型。**注意**：此任务完成后，`src/crawler.go` 和 `cmd/crawler/main.go` 中对 `src.Video`/`src.Author` 的引用会编译失败，需要在此任务中同步修改这些引用（改为引用 bilibili 包），或者采用类型别名过渡。
  
  **策略**：在 `src/types.go` 中保留类型别名（`type Video = bilibili.Video` 等）作为过渡，确保编译通过。后续任务逐步消除这些别名。
- context:
  - `src/types.go` — 直接修改，移出类型 + 添加别名
  - `src/platform/bilibili/types.go` — 新建，承接类型定义
  - `src/stats/stats.go` — 上游，引用 `src.VideoDetail`/`src.AuthorStats`/`src.TopVideo`
  - `src/platform/bilibili/csv.go` — 上游，引用 `src.Author`/`src.Video`
  - `src/platform/bilibili/search.go` — 上游，引用 `src.Video`
  - `src/platform/bilibili/author.go` — 上游，引用 `src.AuthorInfo`/`src.VideoDetail`
  - `src/crawler.go` — 下游，引用 `src.Video`/`src.Author` 等
  - `cmd/crawler/main.go` — 下游，引用 `src.Video`/`src.Author` 等
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `src/platform/bilibili/types.go` 包含 `Video`、`AuthorInfo`、`VideoDetail`、`AuthorStats`、`TopVideo`、`Author`
  - [ ] `src/types.go` 中这些类型已移除（通过别名过渡）
  - [ ] `src/stats/stats.go` 改为引用 bilibili 包的类型
- 子任务:
  - [ ] 2.1: 创建 `src/platform/bilibili/types.go`，定义所有 Bilibili 平台类型
  - [ ] 2.2: 修改 `src/types.go`，删除原始类型定义，添加类型别名指向 bilibili 包
  - [ ] 2.3: 修改 `src/stats/stats.go`，改为引用 bilibili 包的类型（或通过别名透明引用）
  - [ ] 2.4: 确认 bilibili 包内部（csv.go, search.go, author.go）改为引用本包类型

### 任务 3: [x] ReadVideoCSV 返回 []AuthorMid + Bilibili CSV adapter 适配

- 文件: `src/export/export.go`（修改）, `src/platform/bilibili/csv.go`（修改）
- 依赖: Task 1, 2
- spec 映射: FR-2.3 (ReadVideoCSV 返回 []AuthorMid), FR-2.2 (CSV adapter 废弃)
- 说明:
  1. 新增 `ReadVideoCSVAuthors(csvPath string, authorIDCol, authorNameCol int) ([]AuthorMid, error)` 函数，从 CSV 中提取 AuthorMid 列表。旧的 `ReadVideoCSV` 暂时保留。
  2. 修改 Bilibili 的 `csv.go`：`BilibiliVideoCSVAdapter` 和 `BilibiliAuthorCSVAdapter` 改为不依赖 `src.Video`/`src.Author`，而是使用本包的类型 + 提供 `ToRow` 方法返回 `[]string`。同时提供 `VideoHeader()`、`AuthorBasicHeader()`、`AuthorFullHeader()` 等函数。
- context:
  - `src/export/export.go` — 直接修改，新增 `ReadVideoCSVAuthors`
  - `src/platform/bilibili/csv.go` — 直接修改，适配通用 CSVWriter
  - `src/crawler.go:RunStage0()` Step 7-8 — 下游，调用 ReadVideoCSV
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `ReadVideoCSVAuthors` 能正确从 CSV 提取 AuthorMid 列表
  - [ ] Bilibili CSV adapter 的 `Row`/`BasicRow`/`FullRow` 方法使用本包类型
- 子任务:
  - [ ] 3.1: 新增 `ReadVideoCSVAuthors(csvPath string, authorIDCol, authorNameCol int) ([]src.AuthorMid, error)`
  - [ ] 3.2: 修改 Bilibili `csv.go`，`Row`/`BasicRow`/`FullRow` 改为接收本包类型
  - [ ] 3.3: 确保旧的 `ReadVideoCSV` 和旧 adapter 接口仍可编译（过渡期）

### 任务 4: [x] SearchRecorder 接口 + Bilibili 实现

- 文件: `src/types.go`（修改）, `src/platform/bilibili/search.go`（修改）
- 依赖: Task 1, 2, 3
- spec 映射: FR-1.1 (SearchRecorder 接口)
- 说明:
  1. 在 `src/types.go` 中新增 `SearchRecorder` 接口定义
  2. 在 Bilibili `search.go` 中实现 `SearchAndRecord` 方法：将 `RunStage0` 中的 Step 3~5（先抓第一页 → 并发抓剩余页 → 写 CSV）逻辑下沉到此方法中。Bilibili 的 `SearchAndRecord` 内部使用 Worker Pool 并发抓取。
  3. 旧的 `SearchPage` 方法保留为内部方法（`searchPage`，小写），供 `SearchAndRecord` 调用。
- context:
  - `src/types.go` — 新增 `SearchRecorder` 接口
  - `src/platform/bilibili/search.go` — 直接修改，新增 `SearchAndRecord` 方法
  - `src/crawler.go:RunStage0()` — 下游消费方（Task 5 迁移）
  - `src/export/export.go:CSVWriter` — 被 SearchAndRecord 使用
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `SearchRecorder` 接口定义在 `src/types.go`
  - [ ] `BiliBrowserSearchCrawler` 实现 `SearchRecorder` 接口（编译时检查）
  - [ ] `SearchAndRecord` 内部实现"先抓第一页 → 并发抓剩余页 → 每页写 CSV"逻辑
- 子任务:
  - [ ] 4.1: 在 `src/types.go` 中新增 `SearchRecorder` 接口
  - [ ] 4.2: 在 Bilibili `search.go` 中实现 `SearchAndRecord` 方法
  - [ ] 4.3: 将 `SearchPage` 降级为内部方法 `searchPage`
  - [ ] 4.4: 添加编译时接口检查

### 任务 5: [x] 编排层 RunStage0/1/2 适配 + AuthorCrawler 接口改造

- 文件: `src/crawler.go`（修改）, `src/types.go`（修改）, `src/platform/bilibili/author.go`（修改）, `src/test/crawler_test.go`（修改）
- 依赖: Task 3, 4
- spec 映射: FR-1.1 (RunStage0 瘦身), FR-1.2 (AuthorCrawler 保持), FR-2.4 (编排层适配)
- 说明:
  1. **RunStage0 瘦身**：删除 Step 3~5 的分页逻辑，改为调用 `SearchRecorder.SearchAndRecord()`。Step 7-8 改为调用 `ReadVideoCSVAuthors()`。
  2. **AuthorCrawler 接口改造**：`FetchAuthorInfo` 返回值从 `*AuthorInfo` 改为 `([]string, error)`（即 CSV 行）。`FetchAllAuthorVideos` 返回值从 `([]VideoDetail, PageInfo, error)` 改为 `([][]string, error)`。
  3. **RunStage1/2 适配**：`processOneAuthorBasic` 不再组装 `Author` 结构体，直接将 `FetchAuthorInfo` 返回的 `[]string` 写入 CSV。
  4. **Stage0Config/Stage1Config/Stage2Config 适配**：移除对旧类型的依赖。
  5. **测试文件适配**：更新 mock 和测试用例。
- context:
  - `src/crawler.go` — 直接修改，RunStage0/1/2 + processOneAuthorBasic/Full + StageConfig
  - `src/types.go` — 修改 AuthorCrawler 接口签名 + StageConfig 类型
  - `src/platform/bilibili/author.go` — 修改 FetchAuthorInfo/FetchAllAuthorVideos 返回值
  - `src/test/crawler_test.go` — 修改 mock + 测试用例
  - `src/platform/bilibili/csv.go` — 上游，提供 toRow 函数
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `RunStage0` 不再包含分页逻辑，只调用 `SearchRecorder.SearchAndRecord()`
  - [ ] `RunStage1` 使用通用 `CSVRowWriter` 写入 `[]string`
  - [ ] `AuthorCrawler.FetchAuthorInfo` 返回 `([]string, error)`
  - [ ] `go test ./src/test/...` 通过
- 子任务:
  - [ ] 5.1: 修改 `AuthorCrawler` 接口签名
  - [ ] 5.2: 修改 Bilibili `author.go` 的 `FetchAuthorInfo` 和 `FetchAllAuthorVideos` 返回值
  - [ ] 5.3: 重写 `RunStage0`：瘦身为调用 `SearchRecorder.SearchAndRecord()` + `ReadVideoCSVAuthors()`
  - [ ] 5.4: 重写 `RunStage1`/`RunStage2`：使用通用 `CSVRowWriter`
  - [ ] 5.5: 删除 `processOneAuthorBasic`/`processOneAuthorFull`（逻辑已内联或下沉）
  - [ ] 5.6: 更新 `Stage0Config`/`Stage1Config`/`Stage2Config`
  - [ ] 5.7: 更新 `src/test/crawler_test.go` 中的 mock 和测试用例

### 任务 6: [x] main.go 适配 + 旧接口/类型清理

- 文件: `cmd/crawler/main.go`（修改）, `src/types.go`（修改）, `src/export/export.go`（修改）
- 依赖: Task 5
- spec 映射: FR-1.1, FR-2.1, FR-2.2 (清理旧代码)
- 说明:
  1. 修改 `main.go`：适配新的 `SearchRecorder` 接口和通用 `CSVRowWriter`，移除对旧 `VideoCSVAdapter`/`AuthorCSVAdapter` 的使用。
  2. 清理 `src/types.go`：删除类型别名（Task 2 添加的过渡别名）、删除旧接口（`SearchCrawler`、`VideoCSVAdapter`、`AuthorCSVAdapter`、`VideoCSVRowWriter`、`AuthorCSVRowWriter`）。
  3. 清理 `src/export/export.go`：删除旧的 `VideoCSVWriter`、`AuthorCSVWriter`、`ReadVideoCSV`。
- context:
  - `cmd/crawler/main.go` — 直接修改，适配新接口
  - `src/types.go` — 直接修改，清理旧接口和别名
  - `src/export/export.go` — 直接修改，清理旧 Writer
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `src/types.go` 中不再有 `Video`、`Author`、`AuthorInfo` 等平台类型（含别名）
  - [ ] `src/types.go` 中不再有 `SearchCrawler`、`VideoCSVAdapter`、`AuthorCSVAdapter` 接口
  - [ ] `src/export/export.go` 中不再有 `VideoCSVWriter`、`AuthorCSVWriter`
  - [ ] `main.go` 使用 `SearchRecorder` 和通用 `CSVRowWriter`
- 子任务:
  - [ ] 6.1: 修改 `main.go` 的 Stage 0 组装逻辑（使用 SearchRecorder）
  - [ ] 6.2: 修改 `main.go` 的 Stage 1/2 组装逻辑（使用通用 CSVRowWriter）
  - [ ] 6.3: 删除 `src/types.go` 中的类型别名和旧接口
  - [ ] 6.4: 删除 `src/export/export.go` 中的旧 Writer 和旧 ReadVideoCSV

### 任务 7: [x] 配置重构 — 分阶段 + 分平台

- 文件: `src/config/config.go`（修改）, `conf/config.yaml`（修改）, `cmd/crawler/main.go`（修改）
- 依赖: Task 6
- spec 映射: FR-4.1 (分阶段配置), FR-4.2 (分平台配置), FR-4.3 (YouTube 筛选配置)
- 说明:
  1. 重构 `Config` 结构体：将全局参数（concurrency、request_interval、max_consecutive_failures）改为分阶段配置（每个 Stage 可独立配置）。
  2. 将平台相关参数（cookie、筛选项）改为分平台配置。
  3. 新增 YouTube 平台配置结构（筛选项预留）。
  4. 更新 `config.yaml` 为新结构。
  5. 更新 `main.go` 读取新配置结构。
- context:
  - `src/config/config.go` — 直接修改，重构 Config 结构体
  - `conf/config.yaml` — 直接修改，新配置格式
  - `cmd/crawler/main.go` — 修改配置读取逻辑
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] 配置支持分阶段的 concurrency、request_interval
  - [ ] 配置支持分平台的 cookie
  - [ ] YouTube 平台配置结构已预留
  - [ ] 旧的 `config.yaml` 格式不再兼容（breaking change，需更新文档）
- 子任务:
  - [ ] 7.1: 重构 `Config` 结构体（分阶段 + 分平台）
  - [ ] 7.2: 更新 `applyDefaults` 和 `clampValues`
  - [ ] 7.3: 更新 `config.yaml`
  - [ ] 7.4: 更新 `main.go` 配置读取

### 任务 8: [x] YouTube 爬虫实现

- 文件: `src/platform/youtube/types.go`（新建）, `src/platform/youtube/csv.go`（新建）, `src/platform/youtube/search.go`（新建）, `src/platform/youtube/author.go`（新建）, `cmd/crawler/main.go`（修改）
- 依赖: Task 7
- spec 映射: FR-3.1 (Stage 0 搜索), FR-3.2 (Stage 1 作者信息), FR-1.3 (Stage 2 空实现)
- 说明:
  1. 创建 YouTube 平台类型定义（`types.go`）
  2. 创建 YouTube CSV 转换逻辑（`csv.go`）：VideoHeader、VideoToRow、AuthorHeader、AuthorToRow
  3. 实现 YouTube `SearchRecorder`（`search.go`）：滚动加载 + ytInitialData 解析
  4. 实现 YouTube `AuthorCrawler`（`author.go`）：SSR 提取 + browse API
  5. 在 `main.go` 中注册 YouTube 平台
- context:
  - `src/platform/youtube/` — 新建目录和文件
  - `cmd/crawler/main.go` — 修改，注册 YouTube 平台
  - `src/types.go` — 上游，定义 SearchRecorder/AuthorCrawler 接口
  - `src/export/export.go` — 上游，提供 CSVWriter
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] YouTube `SearchRecorder` 实现 `SearchAndRecord` 方法
  - [ ] YouTube `AuthorCrawler` 实现 `FetchAuthorInfo` 和 `FetchAllAuthorVideos`（空实现）
  - [ ] `main.go` 支持 `--platform youtube`
  - [ ] YouTube Stage 2 为空实现（返回空列表）
- 子任务:
  - [ ] 8.1: 创建 `src/platform/youtube/types.go`
  - [ ] 8.2: 创建 `src/platform/youtube/csv.go`
  - [ ] 8.3: 创建 `src/platform/youtube/search.go`（SearchRecorder 实现）
  - [ ] 8.4: 创建 `src/platform/youtube/author.go`（AuthorCrawler 实现）
  - [ ] 8.5: 修改 `main.go` 注册 YouTube 平台

---

## Spec 覆盖映射

| Spec 章节 | 任务 | 说明 |
|-----------|------|------|
| FR-1.1 SearchRecorder 接口 | Task 4, 5 | Task 4 定义接口 + Bilibili 实现，Task 5 编排层适配 |
| FR-1.2 AuthorCrawler 保持 | Task 5 | 接口签名调整（返回 []string） |
| FR-1.3 Stage 2 空实现 | Task 8 | YouTube 的 FetchAllAuthorVideos 返回空列表 |
| FR-2.1 平台类型下沉 | Task 2, 6 | Task 2 移动类型 + 别名过渡，Task 6 清理别名 |
| FR-2.2 CSV Writer 通用化 | Task 1, 6 | Task 1 创建通用 Writer，Task 6 清理旧 Writer |
| FR-2.3 ReadVideoCSV 返回 []AuthorMid | Task 3, 5 | Task 3 新增函数，Task 5 编排层迁移 |
| FR-2.4 编排层适配 | Task 5 | processOneAuthorBasic 等函数改造 |
| FR-3.1 YouTube Stage 0 | Task 8 | YouTube SearchRecorder 实现 |
| FR-3.2 YouTube Stage 1 | Task 8 | YouTube AuthorCrawler 实现 |
| FR-4.1 分阶段配置 | Task 7 | Config 结构体重构 |
| FR-4.2 分平台配置 | Task 7 | Config 结构体重构 |
| FR-4.3 YouTube 筛选配置 | Task 7 | 预留配置结构 |
| NFR-1 编译兼容 | 所有 Task | 每个 Task 完成后 go build 通过 |
| NFR-2 向后兼容 | Task 2, 5, 6 | Bilibili 功能不受影响 |
| NFR-3 断点续爬 | Task 4, 5 | Progress 传入 SearchAndRecord |
| NFR-4 无需登录 | Task 8 | YouTube 不需要 cookie |
