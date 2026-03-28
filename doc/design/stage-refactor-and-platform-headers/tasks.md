# 实施任务清单

> 由 spec.md 生成
> 任务总数: 8
> 核心原则: 自底向上——先扩展底层数据模型和接口定义，再改平台层实现，最后改编排层和入口

## 依赖关系总览

```
Task 1 (src/types.go: 扩展数据模型 + 新增 CSV 适配器接口)
  ↓
Task 2 (bilibili/types.go: 新增 UpStatResp 结构体)
  ↓
Task 3 (bilibili/author.go: FetchAuthorInfo 扩展拦截 upstat + arc/search)  ← 依赖 Task 1, 2
  ↓
Task 4 (bilibili/csv.go: 🆕 B 站 CSV 适配器实现)  ← 依赖 Task 1
  ↓
Task 5 (src/export/export.go: CSV Writer 参数化，移除硬编码 header/row)  ← 依赖 Task 1, 4
  ↓
Task 6 (src/stats/stats.go: 移除无效字段 + videoURLPrefix 参数化)  ← 依赖 Task 1
  ↓
Task 7 (src/crawler.go: Stage 拆分 + 重试逻辑抽取)  ← 依赖 Task 1, 5, 6
  ↓
Task 8 (cmd/crawler/main.go + src/progress/progress.go: CLI 入口 + 进度支持)  ← 依赖 Task 7
```

## 变更影响概览

### 文件变更清单

| 文件 | 操作 | 涉及任务 | 说明 |
|------|------|---------|------|
| `src/types.go` | 修改 | Task 1 | AuthorInfo/Author 扩展字段；AuthorStats 移除 AvgLikeCount；新增 CSV 适配器接口 |
| `src/platform/bilibili/types.go` | 修改 | Task 2 | 新增 UpStatResp/UpStatData/UpStatArchive 结构体 |
| `src/platform/bilibili/author.go` | 修改 | Task 3 | FetchAuthorInfo 新增拦截 upstat + arc/search |
| `src/platform/bilibili/csv.go` | 新建 | Task 4 | B 站 CSV 适配器（BasicHeader/FullHeader/Row 转换） |
| `src/export/export.go` | 修改 | Task 5 | AuthorCSVWriter/VideoCSVWriter 接受外部 header+toRow；移除硬编码 header/row/topVideoHyperlink |
| `src/stats/stats.go` | 修改 | Task 6 | 移除 AvgLikeCount；videoURLPrefix 参数化；移除 bilibiliVideoURLPrefix 常量 |
| `src/crawler.go` | 修改 | Task 7 | 拆分 RunStage1→RunStage1+RunStage2；拆分 processOneAuthorOnce→Basic+Full；抽取 retryWithCooldown；Stage1Config/Stage2Config |
| `cmd/crawler/main.go` | 修改 | Task 8 | --stage 支持 0/1/2/all；注入 CSV 适配器；Stage2Config 构建 |
| `src/progress/progress.go` | 修改 | Task 8 | Stage 字段支持 0/1/2 |

## 任务列表

### 任务 1: [x] 扩展通用数据模型 + 新增 CSV 适配器接口 (`src/types.go`)
- 文件: `src/types.go`（修改）
- 依赖: 无
- spec 映射: spec 4.2.1(1) — `src/types.go` 通用数据模型 + CSV 适配器接口
- 说明:
  1. `AuthorInfo` 新增 `TotalLikes int64`、`TotalPlayCount int64`、`VideoCount int` 三个字段
  2. `AuthorStats` 移除 `AvgLikeCount`（探针确认视频列表 API 不返回 like 字段）
  3. `Author` 新增 `TotalLikes int64`、`TotalPlayCount int64` 两个字段
  4. 新增 `AuthorCSVAdapter` 接口（BasicHeader/BasicRow/FullHeader/FullRow）
  5. 新增 `VideoCSVAdapter` 接口（Header/Row）
- context:
  - `src/types.go` — 直接修改目标
  - `src/crawler.go:processOneAuthorOnce()` — 上游：构造 Author 结构体
  - `src/export/export.go:authorToRow()` — 下游：消费 Author 结构体
  - `src/stats/stats.go:CalcAuthorStats()` — 下游：返回 AuthorStats
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] AuthorInfo 包含 TotalLikes、TotalPlayCount、VideoCount 字段
  - [ ] AuthorStats 不包含 AvgLikeCount 字段
  - [ ] Author 包含 TotalLikes、TotalPlayCount 字段
  - [ ] AuthorCSVAdapter 和 VideoCSVAdapter 接口已定义
- 子任务:
  - [ ] 1.1: AuthorInfo 新增 3 个字段
  - [ ] 1.2: AuthorStats 移除 AvgLikeCount
  - [ ] 1.3: Author 新增 2 个字段
  - [ ] 1.4: 新增 AuthorCSVAdapter 接口定义
  - [ ] 1.5: 新增 VideoCSVAdapter 接口定义
  - [ ] 1.6: 修复因 AvgLikeCount 移除导致的编译错误（stats.go 和 export.go 中的引用）

### 任务 2: [x] 新增 B 站 UpStat API 响应结构体 (`bilibili/types.go`)
- 文件: `src/platform/bilibili/types.go`（修改）
- 依赖: 无
- spec 映射: spec 4.2.1(2) — `bilibili/types.go` B 站 API 响应结构体
- 说明:
  1. 新增 `UpStatResp` 结构体（拦截 `/x/space/upstat` API 的响应）
  2. 新增 `UpStatData` 结构体（包含 Archive 和 Likes 字段）
  3. 新增 `UpStatArchive` 结构体（包含 View 字段 = 总播放数）
- context:
  - `src/platform/bilibili/types.go` — 直接修改目标
  - `src/platform/bilibili/author.go:FetchAuthorInfo()` — 下游：解析 upstat 响应
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] UpStatResp/UpStatData/UpStatArchive 结构体已定义，JSON tag 与 API 返回一致
- 子任务:
  - [ ] 2.1: 新增 UpStatResp、UpStatData、UpStatArchive 三个结构体

### 任务 3: [x] FetchAuthorInfo 扩展拦截 upstat + arc/search (`bilibili/author.go`)
- 文件: `src/platform/bilibili/author.go`（修改）
- 依赖: Task 1, Task 2
- spec 映射: spec 4.2.1(3) + 4.3.3 — `bilibili/author.go` FetchAuthorInfo 扩展
- 说明:
  1. `FetchAuthorInfo` 的 rules 新增拦截 `/x/space/upstat`（ID: "up_stat"）和 `/x/space/wbi/arc/search`（ID: "video_list"）
  2. 解析 upstat 响应获取 TotalLikes 和 TotalPlayCount
  3. 解析 arc/search 响应只取 `data.page.count` 作为 VideoCount
  4. 四个 API 都必须成功拦截，任一失败直接返回错误（不降级）
  5. 返回的 `AuthorInfo` 填充新增的 3 个字段
- context:
  - `src/platform/bilibili/author.go:FetchAuthorInfo()` — 直接修改目标
  - `src/platform/bilibili/types.go:UpStatResp` — 新增的响应结构体
  - `src/types.go:AuthorInfo` — 返回值类型（已扩展字段）
  - `src/crawler.go:processOneAuthorOnce()` — 上游调用方
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] FetchAuthorInfo 拦截 4 个 API（acc/info, relation/stat, upstat, arc/search）
  - [ ] 返回的 AuthorInfo 包含 TotalLikes、TotalPlayCount、VideoCount
- 子任务:
  - [ ] 3.1: 扩展 rules 数组，新增 upstat 和 arc/search 拦截规则
  - [ ] 3.2: 新增 upStatBody 和 videoListBody 的提取逻辑
  - [ ] 3.3: 新增 4 个 API 的 nil 检查（不降级）
  - [ ] 3.4: 解析 upstat 响应（UpStatResp）
  - [ ] 3.5: 解析 arc/search 响应（VideoListResp，只取 page.count）
  - [ ] 3.6: 填充 AuthorInfo 的 TotalLikes、TotalPlayCount、VideoCount

### 任务 4: [x] 新建 B 站 CSV 适配器 (`bilibili/csv.go`)
- 文件: `src/platform/bilibili/csv.go`（新建）
- 依赖: Task 1
- spec 映射: spec 4.2.1(4) — `bilibili/csv.go` B 站 CSV 适配器
- 说明:
  1. 新建 `bilibili/csv.go` 文件
  2. 实现 `BilibiliAuthorCSVAdapter`（实现 `src.AuthorCSVAdapter` 接口）
     - `BasicHeader()`: 8 列（博主名字、ID、粉丝数、总获赞数、总播放数、视频数量、视频平均播放量、视频平均点赞量）
     - `BasicRow()`: 平均播放量/点赞量实时计算（TotalPlayCount/VideoCount）
     - `FullHeader()`: 13 列（Basic 8 列 + 视频平均评论数、视频平均时长、TOP1/2/3）
     - `FullRow()`: 包含 Stats 和 TopVideos 数据
  3. 实现 `BilibiliVideoCSVAdapter`（实现 `src.VideoCSVAdapter` 接口）
  4. 迁移 `bilibiliVideoURLPrefix` 常量到此文件（导出为 `VideoURLPrefix`）
  5. 迁移 `topVideoHyperlink` 函数到此文件
  6. 新增 `safeDiv` 辅助函数
- context:
  - `src/platform/bilibili/csv.go` — 新建文件
  - `src/types.go:AuthorCSVAdapter/VideoCSVAdapter` — 实现的接口
  - `src/export/export.go:topVideoHyperlink()` — 迁移来源
  - `src/stats/stats.go:bilibiliVideoURLPrefix` — 迁移来源
  - `cmd/crawler/main.go` — 上游：注入适配器
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] BilibiliAuthorCSVAdapter 实现 AuthorCSVAdapter 接口（编译时检查）
  - [ ] BilibiliVideoCSVAdapter 实现 VideoCSVAdapter 接口（编译时检查）
  - [ ] BasicHeader 返回 8 列，FullHeader 返回 13 列
- 子任务:
  - [ ] 4.1: 创建文件，定义 VideoURLPrefix 常量
  - [ ] 4.2: 实现 BilibiliAuthorCSVAdapter（BasicHeader/BasicRow/FullHeader/FullRow）
  - [ ] 4.3: 实现 BilibiliVideoCSVAdapter（Header/Row）
  - [ ] 4.4: 实现 safeDiv 和 topVideoHyperlink 辅助函数
  - [ ] 4.5: 添加编译时接口检查（var _ src.AuthorCSVAdapter = ...）

### 任务 5: [x] CSV Writer 参数化，移除硬编码 header/row (`src/export/export.go`)
- 文件: `src/export/export.go`（修改）
- 依赖: Task 1, Task 4
- spec 映射: spec 4.2.1(5) + 4.3.5 — `src/export/export.go` 通用 CSV Writer
- 说明:
  1. `NewAuthorCSVWriter` 新增 `header []string` 和 `toRow func(src.Author) []string` 参数
  2. `AuthorCSVWriter` 内部存储 `toRow` 函数，`WriteRow` 使用注入的 `toRow`
  3. `NewVideoCSVWriter` 新增 `header []string` 和 `toRow func(src.Video) []string` 参数
  4. `VideoCSVWriter` 内部存储 `toRow` 函数，`WriteRows` 使用注入的 `toRow`
  5. 移除 `var videoCSVHeader`、`func videoToRow`、`var authorCSVHeader`、`func authorToRow`、`func topVideoHyperlink`（已迁移到 bilibili/csv.go）
  6. `WriteVideoCSV` 和 `WriteAuthorCSV` 批量写入函数也需要接受 header+toRow 参数（或删除，如果不再使用）
  7. `OpenAuthorCSVWriter` 和 `OpenVideoCSVWriter` 也需要接受 `toRow` 参数
- context:
  - `src/export/export.go` — 直接修改目标
  - `src/platform/bilibili/csv.go` — 提供 header 和 toRow 实现
  - `cmd/crawler/main.go` — 上游：构造 Writer 时传入 header/toRow
  - `src/crawler.go:RunStage0/RunStage1` — 上游：通过 Config 间接使用 Writer
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] NewAuthorCSVWriter 签名包含 header 和 toRow 参数
  - [ ] NewVideoCSVWriter 签名包含 header 和 toRow 参数
  - [ ] 文件中不再包含 videoCSVHeader、authorCSVHeader、videoToRow、authorToRow、topVideoHyperlink
- 子任务:
  - [ ] 5.1: AuthorCSVWriter 新增 toRow 字段，修改 NewAuthorCSVWriter 签名
  - [ ] 5.2: AuthorCSVWriter.WriteRow 使用注入的 toRow
  - [ ] 5.3: OpenAuthorCSVWriter 新增 toRow 参数
  - [ ] 5.4: VideoCSVWriter 新增 toRow 字段，修改 NewVideoCSVWriter 签名
  - [ ] 5.5: VideoCSVWriter.WriteRows 使用注入的 toRow
  - [ ] 5.6: OpenVideoCSVWriter 新增 toRow 参数
  - [ ] 5.7: 移除 videoCSVHeader、videoToRow、authorCSVHeader、authorToRow、topVideoHyperlink
  - [ ] 5.8: 更新或移除 WriteVideoCSV/WriteAuthorCSV 批量写入函数

### 任务 6: [x] stats.go 移除无效字段 + videoURLPrefix 参数化 (`src/stats/stats.go`)
- 文件: `src/stats/stats.go`（修改）
- 依赖: Task 1
- spec 映射: spec 4.3.4 — `CalcAuthorStats` 扩展
- 说明:
  1. 移除 `bilibiliVideoURLPrefix` 常量（已迁移到 bilibili/csv.go）
  2. `CalcAuthorStats` 新增 `videoURLPrefix string` 参数
  3. 移除 `AvgLikeCount` 的计算逻辑（AuthorStats 已移除该字段）
  4. TopVideo URL 生成使用传入的 `videoURLPrefix` 参数
- context:
  - `src/stats/stats.go` — 直接修改目标
  - `src/platform/bilibili/csv.go:VideoURLPrefix` — 迁移目标
  - `cmd/crawler/main.go` — 上游：通过闭包传入 videoURLPrefix
  - `src/crawler.go:Stage1Config.CalcAuthorStats` — 上游：函数签名变更
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] CalcAuthorStats 签名包含 videoURLPrefix 参数
  - [ ] 不再包含 bilibiliVideoURLPrefix 常量
  - [ ] 不再计算 AvgLikeCount
- 子任务:
  - [ ] 6.1: 移除 bilibiliVideoURLPrefix 常量
  - [ ] 6.2: CalcAuthorStats 新增 videoURLPrefix 参数
  - [ ] 6.3: 移除 AvgLikeCount 计算
  - [ ] 6.4: TopVideo URL 使用参数化的 videoURLPrefix

### 任务 7: [x] 编排层 Stage 拆分 + 重试逻辑抽取 (`src/crawler.go`)
- 文件: `src/crawler.go`（修改）
- 依赖: Task 1, Task 5, Task 6
- spec 映射: spec 4.2.1(6) + 4.3.1 + 4.3.2 — 编排层
- 说明:
  1. 新增 `Stage2Config` 结构体（包含 Cooldown、CalcAuthorStats、MaxVideoPerAuthor 等）
  2. 修改 `Stage1Config`：移除 CalcAuthorStats、MaxVideoPerAuthor、VideoPageSize、Cooldown（Stage 1 不遍历视频）
  3. 抽取 `retryWithCooldown` 泛型函数（从 processOneAuthor 中提取重试逻辑）
  4. 拆分 `processOneAuthorOnce` → `processOneAuthorBasic`（Stage 1）+ `processOneAuthorFull`（Stage 2）
  5. 修改 `RunStage1` 为精简版（无重试、无 Cooldown、无视频遍历）
  6. 新增 `RunStage2`（完整版，含重试 + Cooldown + 视频遍历）
  7. 移除旧的 `processOneAuthor` 和 `processOneAuthorOnce`
  8. `Stage1Config.NewAuthorCSVWriter` 和 `Stage2Config.NewAuthorCSVWriter` 签名不变（由 main.go 闭包注入不同的 header/toRow）
  9. `CalcAuthorStats` 函数签名变更：新增 videoURLPrefix 参数（通过 Stage2Config 的闭包适配）
- context:
  - `src/crawler.go` — 直接修改目标
  - `src/types.go` — AuthorCSVAdapter 接口、扩展后的 Author/AuthorInfo
  - `src/export/export.go` — 参数化后的 CSV Writer
  - `src/stats/stats.go` — 参数化后的 CalcAuthorStats
  - `cmd/crawler/main.go` — 上游：构建 Stage1Config/Stage2Config
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] Stage1Config 不包含 CalcAuthorStats、MaxVideoPerAuthor、VideoPageSize、Cooldown
  - [ ] Stage2Config 包含 CalcAuthorStats、MaxVideoPerAuthor、Cooldown
  - [ ] retryWithCooldown 泛型函数已定义
  - [ ] processOneAuthorBasic 只调用 FetchAuthorInfo
  - [ ] processOneAuthorFull 调用 FetchAuthorInfo + FetchAllAuthorVideos + CalcAuthorStats
  - [ ] RunStage1 无重试、无 Cooldown
  - [ ] RunStage2 有重试 + Cooldown
- 子任务:
  - [ ] 7.1: 新增 Stage2Config 结构体
  - [ ] 7.2: 精简 Stage1Config（移除视频遍历相关字段）
  - [ ] 7.3: 抽取 retryWithCooldown 泛型函数
  - [ ] 7.4: 实现 processOneAuthorBasic（Stage 1 路径）
  - [ ] 7.5: 实现 processOneAuthorFull（Stage 2 路径）
  - [ ] 7.6: 重写 RunStage1（精简版，无重试/Cooldown）
  - [ ] 7.7: 新增 RunStage2（完整版，含重试/Cooldown）
  - [ ] 7.8: 移除旧的 processOneAuthor 和 processOneAuthorOnce

### 任务 8: [x] CLI 入口改造 + 进度支持 (`cmd/crawler/main.go` + `src/progress/progress.go`)
- 文件: `cmd/crawler/main.go`（修改）, `src/progress/progress.go`（修改）
- 依赖: Task 7
- spec 映射: spec 4.2.1(7) + 4.3.6 — CLI 入口 + --stage 参数语义
- 说明:
  1. `--stage` 验证支持 `0|1|2|all`
  2. 创建 `BilibiliAuthorCSVAdapter` 和 `BilibiliVideoCSVAdapter` 实例
  3. Stage 1 注入 BasicHeader/BasicRow，Stage 2 注入 FullHeader/FullRow
  4. `--stage all` = Stage 0 + Stage 2（跳过独立 Stage 1）
  5. `--stage 1` = 只跑 Stage 1（精简版）
  6. `--stage 2` = 只跑 Stage 2（完整版，内含 FetchAuthorInfo）
  7. Stage2Config 的 CalcAuthorStats 通过闭包注入 `bilibili.VideoURLPrefix`
  8. VideoCSVWriter 的构造也改为注入 header/toRow
  9. `progress.go`：Stage 字段语义更新（0=搜索, 1=基本博主信息, 2=完整博主信息）
- context:
  - `cmd/crawler/main.go` — 直接修改目标
  - `src/progress/progress.go` — 直接修改目标
  - `src/crawler.go:Stage1Config/Stage2Config/RunStage1/RunStage2` — 下游
  - `src/platform/bilibili/csv.go` — 提供 CSV 适配器
  - `src/export/export.go` — 参数化后的 Writer 构造函数
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `--stage 2` 和 `--stage all` 被正确识别
  - [ ] `--stage all` 执行 Stage 0 + Stage 2（不单独执行 Stage 1）
  - [ ] `--stage 1` 执行精简版 Stage 1
  - [ ] CSV 适配器正确注入到 Stage1Config/Stage2Config
- 子任务:
  - [ ] 8.1: 修改 --stage 验证逻辑，支持 0/1/2/all
  - [ ] 8.2: 创建 BilibiliAuthorCSVAdapter 和 BilibiliVideoCSVAdapter 实例
  - [ ] 8.3: 构建 Stage1Config（注入 BasicHeader/BasicRow）
  - [ ] 8.4: 构建 Stage2Config（注入 FullHeader/FullRow + CalcAuthorStats 闭包）
  - [ ] 8.5: 修改 Stage 调度逻辑（all = Stage 0 + Stage 2）
  - [ ] 8.6: VideoCSVWriter 构造注入 header/toRow
  - [ ] 8.7: 修改 progress.go 的 SetAuthorMids 等方法支持 Stage 2

---

## Spec 覆盖映射

| Spec 章节 | 任务 | 说明 |
|-----------|------|------|
| 4.2.1(1) src/types.go | Task 1 | AuthorInfo/Author 扩展 + CSV 适配器接口 |
| 4.2.1(2) bilibili/types.go | Task 2 | UpStatResp 结构体 |
| 4.2.1(3) bilibili/author.go | Task 3 | FetchAuthorInfo 扩展 |
| 4.2.1(4) bilibili/csv.go | Task 4 | B 站 CSV 适配器 |
| 4.2.1(5) src/export/export.go | Task 5 | CSV Writer 参数化 |
| 4.2.2 接口设计 | Task 1, 4 | CSV 适配器接口定义 + 实现 |
| 4.2.3 数据模型 | Task 1, 4 | AuthorInfo/Author 扩展 + CSV 列定义 |
| 4.2.4 并发模型 | Task 7 | Stage 1 无 Cooldown, Stage 2 有 Cooldown |
| 4.2.5 错误处理 | Task 3, 7 | upstat 不降级 + retryWithCooldown |
| 4.3.1 重试逻辑抽取 | Task 7 | retryWithCooldown 泛型函数 |
| 4.3.2 Stage 拆分 | Task 7 | RunStage1/RunStage2 + processOneAuthorBasic/Full |
| 4.3.3 FetchAuthorInfo 扩展 | Task 3 | 新增拦截 upstat + arc/search |
| 4.3.4 CalcAuthorStats 扩展 | Task 6 | 移除无效字段 + videoURLPrefix 参数化 |
| 4.3.5 CSV 适配器注入 | Task 5, 8 | Writer 参数化 + main.go 注入 |
| 4.3.6 --stage 参数语义 | Task 8 | CLI 调度逻辑 |
| 4.3.7 边界情况 | Task 3, 7, 8 | 分散在各任务的错误处理中 |
