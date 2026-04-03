# 实施任务清单

> 由 spec.md 生成
> 任务总数: 5
> 核心原则: 先建后迁后删——先重构配置层（新增字段+getter），再迁移调用方，最后删除旧代码

## 依赖关系总览

```
Task 1 (config.go: 重构配置结构体和 getter)
  ↓
Task 2 (config.yaml: 更新配置文件)  ← 依赖 Task 1
  ↓
Task 3 (crawler.go + main.go: 适配编排层)  ← 依赖 Task 1
  ↓
Task 4 (bilibili/search.go: 适配 Bilibili 搜索)  ← 依赖 Task 1
  ↓
Task 5 (youtube/search.go: 适配 YouTube 搜索)  ← 依赖 Task 1
```

> Task 2~5 之间无互相依赖，可并行执行（均只依赖 Task 1）。
> 但为保证每步可编译，按 Task 2 → 3 → 4 → 5 顺序执行。

## 变更影响概览

### 文件变更清单

| 文件 | 操作 | 涉及任务 | 说明 |
|------|------|---------|------|
| `src/config/config.go` | 修改 | Task 1 | 核心：结构体重构、getter 重写、defaults/clamp 更新 |
| `conf/config.yaml` | 修改 | Task 2 | 配置文件更新为新结构 |
| `src/crawler.go` | 修改 | Task 3 | Stage1Config/Stage2Config 删除并发相关字段，内部改用 config.Get() |
| `cmd/crawler/main.go` | 修改 | Task 3 | browser.New 并发数改为平台级；删除 Stage 并发参数传递 |
| `src/platform/bilibili/search.go` | 修改 | Task 4 | MaxSearchPage → MaxSearchVideos 换算；并发数改为平台级 |
| `src/platform/youtube/search.go` | 修改 | Task 5 | 删除 maxScroll 逻辑，改用 MaxSearchVideos 退出条件 |

## 任务列表

### 任务 1: [x] config.go 重构配置结构体和 getter

- 文件: `src/config/config.go`（修改）
- 依赖: 无
- spec 映射: 5.1 配置结构体重构, 5.2 Getter 方法变更, 5.3 默认值和校验变更
- 说明:
  1. 删除 `StageConfig` 结构体
  2. 删除 `Config` 中的 `Stage0`、`Stage1`、`Stage2` 字段
  3. 删除 `Config` 中的 `MaxSearchPage` 字段，新增 `MaxSearchVideos` 字段
  4. 删除 `YouTubeConfig` 中的 `MaxScrollCount` 字段
  5. 在 `BilibiliConfig` 和 `YouTubeConfig` 中新增 `Concurrency` 和 `RequestInterval` 字段
  6. 删除 12 个 Stage getter 方法（`GetStage0/1/2Concurrency/RequestInterval/MaxConsecutiveFailures`）
  7. 新增 `GetPlatformConcurrency(platform string) int` 和 `GetPlatformRequestInterval(platform string) time.Duration`
  8. 更新常量：删除 `DefaultMaxSearchPage`/`MinMaxSearchPage`/`MaxMaxSearchPage`，新增 `DefaultMaxSearchVideos`/`MinMaxSearchVideos`/`MaxMaxSearchVideos`
  9. 更新 `applyDefaults`：`MaxSearchPage` → `MaxSearchVideos`
  10. 更新 `clampValues`：删除 Stage clamp，新增 `MaxSearchVideos` clamp 和平台级 Concurrency clamp
- context:
  - `src/config/config.go` — 直接修改目标
  - `cmd/crawler/main.go:main()` — 上游调用方（读取 config getter）
  - `src/crawler.go:RunStage1/RunStage2` — 上游调用方（读取 config getter）
  - `src/platform/bilibili/search.go:SearchAndRecord` — 上游调用方（读取 MaxSearchPage）
  - `src/platform/youtube/search.go:SearchAndRecord` — 上游调用方（读取 MaxScrollCount）
- 验收标准:
  - [ ] `go build ./src/config/...` 编译通过
  - [ ] `StageConfig` 结构体不存在
  - [ ] 12 个 Stage getter 不存在
  - [ ] `GetPlatformConcurrency` 和 `GetPlatformRequestInterval` 存在且逻辑正确
  - [ ] `MaxSearchVideos` 默认值 100，范围 [1, 10000]
- 子任务:
  - [ ] 1.1: 删除 `StageConfig` 结构体和 `Config` 中的 Stage0/1/2 字段
  - [ ] 1.2: 删除 `MaxSearchPage`，新增 `MaxSearchVideos`
  - [ ] 1.3: 删除 `YouTubeConfig.MaxScrollCount`，新增平台级并发字段
  - [ ] 1.4: 删除 12 个 Stage getter，新增 2 个平台级 getter
  - [ ] 1.5: 更新常量、`applyDefaults`、`clampValues`

> ⚠️ 注意：此任务完成后，`go build ./...` 会失败（因为调用方还在引用旧 getter），这是预期行为。Task 1 只保证 `go build ./src/config/...` 通过。

### 任务 2: [x] config.yaml 更新为新结构

- 文件: `conf/config.yaml`（修改）
- 依赖: Task 1
- spec 映射: 5.5 数据模型
- 说明:
  1. `max_search_page` → `max_search_videos`
  2. 删除 stage0/1/2 注释块
  3. 在 `platform.bilibili` 下新增 `concurrency` 和 `request_interval`
  4. 在 `platform.youtube` 下新增 `concurrency` 和 `request_interval`
  5. 删除 `max_scroll_count` 注释
  6. 更新注释说明
- context:
  - `conf/config.yaml` — 直接修改目标
  - `src/config/config.go` — 配置结构体定义（Task 1 已更新）
- 验收标准:
  - [ ] 配置文件可被 `config.Load()` 正确加载
  - [ ] 不包含 `max_search_page`、`stage0/1/2`、`max_scroll_count` 字段
  - [ ] 包含 `max_search_videos`、`platform.bilibili.concurrency`、`platform.youtube.concurrency` 等新字段
- 子任务:
  - [ ] 2.1: 替换 `max_search_page` 为 `max_search_videos`
  - [ ] 2.2: 删除 stage 注释块，新增平台级并发配置
  - [ ] 2.3: 清理 YouTube 的 `max_scroll_count` 注释

### 任务 3: [x] crawler.go + main.go 适配编排层

- 文件: `src/crawler.go`（修改）, `cmd/crawler/main.go`（修改）
- 依赖: Task 1
- spec 映射: 5.6 并发模型, 5.8 调用方适配汇总
- 说明:
  **crawler.go**:
  1. `Stage1Config` 删除 `Concurrency`、`MaxConsecutiveFailures`、`RequestInterval` 字段
  2. `Stage2Config` 删除 `Concurrency`、`MaxConsecutiveFailures`、`RequestInterval`、`MaxVideoPerAuthor` 字段
  3. `RunStage1` 内部改为从 `config.Get().GetPlatformConcurrency(cfg.Platform)` 等读取
  4. `RunStage2` 同上
  5. `processOneAuthorFull` 中的 `cfg.RequestInterval` 和 `cfg.MaxVideoPerAuthor` 改为从 `config.Get()` 读取

  **main.go**:
  1. `browser.New` 的 `Concurrency` 改为 `cfg.GetPlatformConcurrency(*platform)`
  2. 构建 `Stage1Config` 时删除 `Concurrency`、`MaxConsecutiveFailures`、`RequestInterval` 赋值
  3. 构建 `Stage2Config` 时删除 `Concurrency`、`MaxVideoPerAuthor`、`MaxConsecutiveFailures`、`RequestInterval` 赋值
  4. `buildPlatformConfig` 中 `ac.SetPaginationInterval` 改为使用 `cfg.GetPlatformRequestInterval(*platform)`
  5. 日志中的 `cfg.Concurrency` 改为 `cfg.GetPlatformConcurrency(*platform)`
- context:
  - `src/crawler.go` — 直接修改目标
  - `cmd/crawler/main.go` — 直接修改目标
  - `src/config/config.go` — 提供新 getter（Task 1 已完成）
  - `src/pool/pool.go` — 下游消费方（pool.Run 接收 concurrency 参数）
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `Stage1Config` 和 `Stage2Config` 不包含 `Concurrency`、`RequestInterval`、`MaxConsecutiveFailures` 字段
  - [ ] `RunStage1`/`RunStage2` 内部从 `config.Get()` 读取并发配置
  - [ ] `browser.New` 使用平台级并发数
- 子任务:
  - [ ] 3.1: 简化 `Stage1Config` 和 `Stage2Config` 结构体
  - [ ] 3.2: 修改 `RunStage1` 内部逻辑
  - [ ] 3.3: 修改 `RunStage2` 和 `processOneAuthorFull` 内部逻辑
  - [ ] 3.4: 修改 `main.go` 中的配置传递

### 任务 4: [x] bilibili/search.go 适配 MaxSearchVideos

- 文件: `src/platform/bilibili/search.go`（修改）
- 依赖: Task 1
- spec 映射: 3.2 FR-1, 3.2 FR-3, 5.8 bilibili/search.go 变更
- 说明:
  1. `cfg.MaxSearchPage` → `cfg.MaxSearchVideos` 换算：`maxPages = ceil(cfg.MaxSearchVideos / pageSize)`，其中 `pageSize = len(firstVideos)`（第一页返回的视频数）
  2. 如果 `pageSize == 0`（第一页无结果），直接返回
  3. Worker Pool 并发数 `cfg.Concurrency` → `cfg.GetPlatformConcurrency("bilibili")`
  4. `cfg.MaxConsecutiveFailures` → `cfg.MaxConsecutiveFailures`（不变，全局统一）
  5. `cfg.RequestInterval` → `cfg.GetPlatformRequestInterval("bilibili")`
- context:
  - `src/platform/bilibili/search.go:SearchAndRecord` — 直接修改目标
  - `src/config/config.go` — 提供新 getter（Task 1 已完成）
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] 不引用 `cfg.MaxSearchPage`
  - [ ] 使用 `cfg.MaxSearchVideos` 换算页数
  - [ ] Worker Pool 使用 `cfg.GetPlatformConcurrency("bilibili")`
- 子任务:
  - [ ] 4.1: MaxSearchPage → MaxSearchVideos 换算逻辑
  - [ ] 4.2: 并发数和请求间隔改为平台级

### 任务 5: [x] youtube/search.go 适配 MaxSearchVideos

- 文件: `src/platform/youtube/search.go`（修改）
- 依赖: Task 1
- spec 映射: 3.2 FR-1, 3.2 FR-2, 5.8 youtube/search.go 变更
- 说明:
  1. 删除 `maxScroll := ytCfg.MaxScrollCount` 及相关退出条件
  2. 新增 `totalVideos >= cfg.MaxSearchVideos` 退出条件（在滚动循环中检查）
  3. `cfg.RequestInterval` → `cfg.GetPlatformRequestInterval("youtube")`
  4. 初始结果也需要检查是否已达到 `MaxSearchVideos` 限制
- context:
  - `src/platform/youtube/search.go:SearchAndRecord` — 直接修改目标
  - `src/config/config.go` — 提供新 getter（Task 1 已完成）
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] 不引用 `ytCfg.MaxScrollCount`
  - [ ] 滚动循环中有 `totalVideos >= cfg.MaxSearchVideos` 退出条件
  - [ ] 使用 `cfg.GetPlatformRequestInterval("youtube")`
- 子任务:
  - [ ] 5.1: 删除 maxScroll 相关逻辑
  - [ ] 5.2: 新增 MaxSearchVideos 退出条件
  - [ ] 5.3: 请求间隔改为平台级

---

## Spec 覆盖映射

| Spec 章节 | 任务 | 说明 |
|-----------|------|------|
| 5.1 配置结构体重构 | Task 1 | 删除旧结构体/字段，新增新字段 |
| 5.2 Getter 方法变更 | Task 1 | 删除 12 个 Stage getter，新增 2 个平台级 getter |
| 5.3 默认值和校验变更 | Task 1 | 更新常量、applyDefaults、clampValues |
| 5.4 接口设计 | — | 不新增接口，无需单独任务 |
| 5.5 数据模型 | Task 2 | config.yaml 更新 |
| 5.6 并发模型 | Task 3 | 并发数来源变更 |
| 5.7 错误处理 | Task 1 | clamp 逻辑更新（包含在 Task 1 中） |
| 5.8 调用方适配 | Task 3, 4, 5 | main.go/crawler.go/bilibili/youtube 适配 |
| FR-1 max_search_page → max_search_videos | Task 1, 4, 5 | 配置定义(T1) + Bilibili 换算(T4) + YouTube 退出条件(T5) |
| FR-2 删除 max_scroll_count | Task 1, 5 | 配置删除(T1) + YouTube 逻辑删除(T5) |
| FR-3 并发配置下沉到平台级 | Task 1, 3, 4, 5 | 配置定义(T1) + 编排层(T3) + 平台层(T4,T5) |
| FR-4 max_video_per_author 全平台生效 | Task 1, 3 | 保持全局配置(T1) + 编排层读取(T3) |
