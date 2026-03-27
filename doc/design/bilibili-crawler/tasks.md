# 实施任务清单

> 由 spec.md 生成
> 任务总数: 10
> 核心原则: 自底向上构建——先建通用基础设施，再建平台实现，最后组装编排层和入口

## 依赖关系总览

```
Task 1 (项目初始化 + 数据模型)
  ↓
Task 2 (配置加载 config/)        ← 依赖 Task 1
  ↓
Task 3 (HTTP 客户端 httpclient/) ← 依赖 Task 2
  ↓
Task 4 (Worker Pool pool/)       ← 依赖 Task 1
  ↓
Task 5 (统计计算 stats/)         ← 依赖 Task 1
  ↓
Task 6 (CSV 导出 export/)        ← 依赖 Task 1
  ↓
Task 7 (断点续爬 progress/)      ← 依赖 Task 1
  ↓
Task 8 (Bilibili 平台实现)       ← 依赖 Task 1, 3
  ↓
Task 9 (编排层 RunStage0/1)      ← 依赖 Task 4, 5, 6, 7, 8
  ↓
Task 10 (CLI 入口 main.go)       ← 依赖 Task 2, 9
```

> 注：Task 4/5/6/7 之间互不依赖，可并行开发。

## 变更影响概览

### 文件变更清单

| 文件 | 操作 | 涉及任务 | 说明 |
|------|------|---------|------|
| `go.mod` | 新建 | Task 1 | Go module 初始化 |
| `go.sum` | 新建 | Task 1, 5 | 依赖锁定 |
| `conf/config.yaml` | 新建 | Task 2 | 默认配置文件 |
| `src/types.go` | 新建 | Task 1 | 通用数据模型 |
| `src/crawler.go` | 新建 | Task 1, 9 | 接口定义 + 编排函数 |
| `src/config/config.go` | 新建 | Task 2 | 配置加载 |
| `src/httpclient/client.go` | 新建 | Task 3 | HTTP 客户端 + 重试 |
| `src/pool/pool.go` | 新建 | Task 4 | 泛型 Worker Pool |
| `src/stats/stats.go` | 新建 | Task 5 | 统计计算 + 语言检测 |
| `src/export/export.go` | 新建 | Task 6 | CSV 导出 |
| `src/progress/progress.go` | 新建 | Task 7 | 断点续爬 |
| `src/platform/bilibili/types.go` | 新建 | Task 8 | Bilibili API 响应结构体 |
| `src/platform/bilibili/search.go` | 新建 | Task 8 | Bilibili 搜索实现 |
| `src/platform/bilibili/author.go` | 新建 | Task 8 | Bilibili 博主详情实现 |
| `src/main.go` | 新建 | Task 10 | CLI 入口 |

## 任务列表

### 任务 1: [x] 项目初始化 + 数据模型 + 接口定义
- 文件: `go.mod`（新建）, `src/types.go`（新建）, `src/crawler.go`（新建）
- 依赖: 无
- spec 映射: §4.2.1 目录结构, §4.2.2 接口设计, §4.2.3 数据模型
- 说明: 初始化 Go module，创建通用数据模型（Video、VideoDetail、AuthorInfo、Author、AuthorStats、TopVideo）和流程框架接口（SearchCrawler、AuthorCrawler）+ 辅助类型（PageInfo、AuthorMid）。编排函数 RunStage0/RunStage1 先留 stub。
- context:
- `go.mod` — Go module 定义，module path 为 `github.com/dylanyuanZ/fast_web_meta_crawler`（或合适的路径）
  - `src/types.go` — 所有平台无关的数据结构
  - `src/crawler.go` — 接口 + PageInfo/AuthorMid + RunStage0/RunStage1 stub
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `src/types.go` 包含 spec §4.2.3 定义的全部 6 个类型
  - [ ] `src/crawler.go` 包含 SearchCrawler、AuthorCrawler 接口 + PageInfo、AuthorMid 类型
  - [ ] RunStage0、RunStage1 为 stub 实现（返回 fmt.Errorf stub 错误）
- 子任务:
  - [ ] 1.1: 创建 `go.mod`，设置 module path 和 Go 版本
  - [ ] 1.2: 创建 `src/types.go`，定义 Video、VideoDetail、AuthorInfo、AuthorStats、TopVideo、Author
  - [ ] 1.3: 创建 `src/crawler.go`，定义 SearchCrawler、AuthorCrawler 接口 + PageInfo、AuthorMid + RunStage0/RunStage1 stub

### 任务 2: [x] 配置加载 (config/)
- 文件: `src/config/config.go`（新建）, `conf/config.yaml`（新建）, `go.mod`（修改，添加 yaml 依赖）
- 依赖: Task 1
- spec 映射: §4.2.1 config/ 模块, §8.2 配置参数
- 说明: 实现配置文件加载，支持 YAML 解析、默认值填充、参数校验（clamp + WARN 日志）。通过 `Load()` 加载配置，`Get()` 获取全局配置对象。创建默认 `conf/config.yaml`。
- context:
  - `src/config/config.go` — Config struct + Load() + Get() + 校验逻辑
  - `conf/config.yaml` — 默认配置文件
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] Config struct 包含 spec §8.2 定义的全部参数（含嵌套 HTTP 配置）
  - [ ] Load() 能正确解析 `conf/config.yaml`
  - [ ] 缺少可选字段时使用默认值
  - [ ] 超出范围的参数被 clamp 到边界值
- 子任务:
  - [ ] 2.1: 定义 Config struct（含 HTTPConfig 嵌套结构）和默认值常量
  - [ ] 2.2: 实现 Load(path) 函数，解析 YAML + 填充默认值 + 参数校验
  - [ ] 2.3: 实现 Get() 全局访问函数
  - [ ] 2.4: 创建 `conf/config.yaml` 默认配置文件

### 任务 3: [x] HTTP 客户端 + 指数退避重试 (httpclient/)
- 文件: `src/httpclient/client.go`（新建）
- 依赖: Task 2
- spec 映射: §4.2.5 第 1 层 httpclient 重试策略
- 说明: 封装 HTTP 客户端，支持指数退避重试。重试判定：5xx/429/超时重试，4xx（除 429）不重试。每次重试输出 WARN 日志。使用 config 中的 HTTP 配置参数。
- context:
  - `src/httpclient/client.go` — Client struct + Get() + 重试逻辑
  - `src/config/config.go` — 读取 HTTP 配置参数
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] Client.Get() 对 5xx/429/超时自动重试，最多 N 次
  - [ ] 4xx（除 429）不重试，直接返回 error
  - [ ] 重试间隔按指数退避增长，不超过 MaxDelay
  - [ ] 每次重试输出 WARN 日志（URL、状态码、重试次数、下次等待时间）
- 子任务:
  - [ ] 3.1: 定义 Client struct，接受 config 中的 HTTP 参数
  - [ ] 3.2: 实现 Get(ctx, url) 方法，含重试判定逻辑
  - [ ] 3.3: 实现指数退避延迟计算

### 任务 4: [x] Worker Pool 并发 (pool/)
- 文件: `src/pool/pool.go`（新建）
- 依赖: Task 1
- spec 映射: §4.2.4 并发模型
- 说明: 实现泛型 Worker Pool，基于 channel + N goroutine 消费模型。支持 ctx 取消、连续失败熔断。返回 []TaskResult 保留原始任务输入。
- context:
  - `src/pool/pool.go` — TaskResult[T,R] + Run[T,R]() + 熔断逻辑
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] Run() 使用泛型，编译期类型安全
  - [ ] 所有任务完成后返回 []TaskResult，成功/失败均有对应结果
  - [ ] ctx 取消时 Worker 退出
  - [ ] 连续失败达到阈值时触发熔断（cancel ctx）
  - [ ] 空任务列表返回空结果，不 panic
- 子任务:
  - [ ] 4.1: 定义 TaskResult[T, R] 泛型结构体
  - [ ] 4.2: 实现 Run[T, R]() 函数，channel 分发 + N goroutine 消费
  - [ ] 4.3: 实现连续失败熔断逻辑（在结果收集阶段检查）

### 任务 5: [x] 统计计算 + 语言检测 (stats/)
- 文件: `src/stats/stats.go`（新建）, `go.mod`（修改，添加 lingua-go 依赖）
- 依赖: Task 1
- spec 映射: §4.3.1 stats 统计计算, §4.3.2 语言检测
- 说明: 实现 CalcAuthorStats() 计算博主统计值 + TOP N 视频，实现 DetectLanguage() 基于 lingua-go 检测视频标题语言。Detector 全局初始化一次。
- context:
  - `src/stats/stats.go` — CalcAuthorStats() + DetectLanguage() + InitDetector()
  - `src/types.go` — 依赖 VideoDetail、AuthorStats、TopVideo 类型
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] CalcAuthorStats 正确计算平均值和 TOP N
  - [ ] CalcAuthorStats 不修改原始切片
  - [ ] DetectLanguage 返回 ISO 639-1 语言代码
  - [ ] 空输入返回零值/unknown，不 panic
- 子任务:
  - [ ] 5.1: 实现 CalcAuthorStats(videos []VideoDetail, topN int) (AuthorStats, []TopVideo)
  - [ ] 5.2: 实现 InitDetector()，初始化 lingua-go Detector（17 种候选语言）
  - [ ] 5.3: 实现 DetectLanguage(titles []string) string

### 任务 6: [x] CSV 导出 (export/)
- 文件: `src/export/export.go`（新建）
- 依赖: Task 1
- spec 映射: §3.1 FR-4 CSV 输出规范, §4.2.1 export/ 模块
- 说明: 实现视频 CSV 和博主 CSV 的导出功能。博主 CSV 的 TOP 视频列使用 Excel HYPERLINK 公式。文件命名格式：`{平台}_{关键词}_{日期}_{时间}_{类型}.csv`。
- context:
  - `src/export/export.go` — WriteVideoCSV() + WriteAuthorCSV() + 文件命名逻辑
  - `src/types.go` — 依赖 Video、Author 类型
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] WriteVideoCSV 输出正确的 CSV（表头 + 数据行，字段顺序符合 spec）
  - [ ] WriteAuthorCSV 输出正确的 CSV，TOP 视频列使用 `=HYPERLINK("url","name")` 公式
  - [ ] 特殊字符（逗号、引号、换行）正确转义
  - [ ] 文件命名格式正确
- 子任务:
  - [ ] 6.1: 实现文件命名函数 GenerateFileName(platform, keyword, fileType string) string
  - [ ] 6.2: 实现 WriteVideoCSV(outputDir string, videos []Video, platform, keyword string) (string, error)
  - [ ] 6.3: 实现 WriteAuthorCSV(outputDir string, authors []Author, platform, keyword string) (string, error)
  - [ ] 6.4: 实现 HYPERLINK 公式生成辅助函数

### 任务 7: [x] 断点续爬 (progress/)
- 文件: `src/progress/progress.go`（新建）
- 依赖: Task 1
- spec 映射: §3.2 NFR-1 断点续爬, §4.3.3 断点续爬
- 说明: 实现进度文件的 JSON 读写，支持阶段 0 逐页记录 + 阶段 1 逐博主记录。原子写入（temp + rename）。任务 hash = MD5(platform_keyword) 前 8 位。
- context:
  - `src/progress/progress.go` — Progress struct + Load() + Save() + MarkDone() + Clean()
  - `src/crawler.go` — 依赖 AuthorMid 类型
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] Progress struct 包含 spec §4.3.3 定义的全部字段
  - [ ] Load() 正确读取并校验进度文件
  - [ ] Save() 使用 temp + rename 原子写入
  - [ ] MarkDone() 更新 DoneAuthors 并持久化
  - [ ] Clean() 删除进度文件
- 子任务:
  - [ ] 7.1: 定义 Progress struct + 任务 hash 计算函数
  - [ ] 7.2: 实现 Load(outputDir, platform, keyword) *Progress
  - [ ] 7.3: 实现 Save(outputDir) error（原子写入）
  - [ ] 7.4: 实现 MarkDone(outputDir, mid) error
  - [ ] 7.5: 实现 Clean(outputDir, platform, keyword) error

### 任务 8: [x] Bilibili 平台实现 (platform/bilibili/)
- 文件: `src/platform/bilibili/types.go`（新建）, `src/platform/bilibili/search.go`（新建）, `src/platform/bilibili/author.go`（新建）
- 依赖: Task 1, 3
- spec 映射: §4.2.1 platform/bilibili/ 模块, §4.2.2 接口与实现的映射
- 说明: 实现 Bilibili 搜索 API 调用（BiliSearchCrawler）和博主详情 API 调用（BiliAuthorCrawler）。定义 Bilibili API 响应结构体。使用 httpclient 发送请求。
- context:
  - `src/platform/bilibili/types.go` — SearchResp, UserInfoResp, VideoListResp 等 API 响应结构体
  - `src/platform/bilibili/search.go` — BiliSearchCrawler 实现 SearchCrawler 接口
  - `src/platform/bilibili/author.go` — BiliAuthorCrawler 实现 AuthorCrawler 接口
  - `src/httpclient/client.go` — 上游依赖，用于发送 HTTP 请求
  - `src/crawler.go` — 上游接口定义
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] BiliSearchCrawler 实现 SearchCrawler 接口（编译器验证）
  - [ ] BiliAuthorCrawler 实现 AuthorCrawler 接口（编译器验证）
  - [ ] API 响应结构体能正确反序列化 Bilibili API JSON 响应
  - [ ] SearchPage 正确解析搜索结果为 []Video + PageInfo
  - [ ] FetchAuthorInfo 正确解析用户信息为 *AuthorInfo
  - [ ] FetchAuthorVideos 正确解析投稿视频为 []VideoDetail + PageInfo
- 子任务:
  - [ ] 8.1: 定义 Bilibili API 响应结构体（SearchResp、UserInfoResp、VideoListResp）
  - [ ] 8.2: 实现 BiliSearchCrawler.SearchPage()
  - [ ] 8.3: 实现 BiliAuthorCrawler.FetchAuthorInfo()
  - [ ] 8.4: 实现 BiliAuthorCrawler.FetchAuthorVideos()

### 任务 9: [x] 编排层 RunStage0 + RunStage1 (crawler.go)
- 文件: `src/crawler.go`（修改，替换 stub 为真实实现）
- 依赖: Task 4, 5, 6, 7, 8
- spec 映射: §4.2.2 编排函数, §4.2.4 阶段 0/1 并发策略, §4.2.5 第 3 层编排层错误汇总
- 说明: 实现 RunStage0（搜索→翻页→汇总→去重→写 CSV + 中间数据文件）和 RunStage1（读取博主列表→Worker Pool 并发采集→统计→写 CSV）。集成断点续爬、熔断、错误汇总、任务完成报告。
- context:
  - `src/crawler.go` — RunStage0() + RunStage1() 真实实现
  - `src/pool/pool.go` — Worker Pool 并发
  - `src/stats/stats.go` — 统计计算 + 语言检测
  - `src/export/export.go` — CSV 导出
  - `src/progress/progress.go` — 断点续爬
  - `src/platform/bilibili/search.go` — SearchCrawler 实现
  - `src/platform/bilibili/author.go` — AuthorCrawler 实现
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] RunStage0 正确实现：串行第一页 → Worker Pool 并发剩余页 → 汇总去重 → 写 CSV + 中间数据文件
  - [ ] RunStage1 正确实现：读取博主列表 → Worker Pool 并发 → 统计 → 写 CSV
  - [ ] 断点续爬集成：阶段 0 逐页更新进度，阶段 1 逐博主更新进度
  - [ ] 错误汇总：任务结束后打印成功/失败统计
  - [ ] 任务完成报告格式符合 spec §8.1
- 子任务:
  - [ ] 9.1: 实现 RunStage0()（搜索翻页 + Worker Pool + 去重 + CSV + 中间数据文件 + 进度更新）
  - [ ] 9.2: 实现 RunStage1()（博主列表 + Worker Pool + processOneAuthor + 统计 + CSV + 进度更新）
  - [ ] 9.3: 实现 processOneAuthor() 内部函数（FetchAuthorInfo + FetchAuthorVideos 翻页 + CalcAuthorStats + DetectLanguage + 组装 Author）
  - [ ] 9.4: 实现错误汇总和任务完成报告输出

### 任务 10: [x] CLI 入口 (main.go)
- 文件: `src/main.go`（新建）
- 依赖: Task 2, 9
- spec 映射: §3.1 FR-3 命令行参数, §4.2.1 main.go 模块, §4.3.3 进度文件生命周期
- 说明: 实现 CLI 入口，解析命令行参数（--platform、--keyword、--stage），加载配置，检测进度文件并提示用户选择，调度阶段执行。
- context:
  - `src/main.go` — main() 函数
  - `src/config/config.go` — 配置加载
  - `src/crawler.go` — RunStage0/RunStage1 编排函数
  - `src/progress/progress.go` — 进度文件检测
  - `src/platform/bilibili/` — 平台实现注册
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `go run src/main.go --platform bilibili --keyword "测试" --stage all` 能正常启动
  - [ ] 缺少必要参数时输出帮助信息
  - [ ] 进度文件存在时提示用户选择（继续/重新开始）
  - [ ] --stage 参数正确控制执行阶段（0/1/all）
  - [ ] 任务完成后输出汇总报告
- 子任务:
  - [ ] 10.1: 解析命令行参数（flag 包）
  - [ ] 10.2: 加载配置文件
  - [ ] 10.3: 进度文件检测 + 用户交互（继续/重新开始）
  - [ ] 10.4: 根据 --platform 创建对应的 Crawler 实现
  - [ ] 10.5: 根据 --stage 调度执行 RunStage0/RunStage1

---

## Spec 覆盖映射

| Spec 章节 | 任务 | 说明 |
|-----------|------|------|
| §4.2.1 目录结构 | Task 1-10 | 所有任务共同构建完整目录结构 |
| §4.2.2 接口设计 | Task 1, 8, 9 | Task 1 定义接口，Task 8 实现，Task 9 编排 |
| §4.2.3 数据模型 | Task 1 | types.go 全部类型 |
| §4.2.4 并发模型 | Task 4, 9 | Task 4 实现 Pool，Task 9 集成使用 |
| §4.2.5 错误处理 | Task 3, 4, 9 | 三层：httpclient 重试 → pool 熔断 → 编排层汇总 |
| §4.3.1 统计计算 | Task 5 | CalcAuthorStats |
| §4.3.2 语言检测 | Task 5 | DetectLanguage + lingua-go |
| §4.3.3 断点续爬 | Task 7, 9, 10 | Task 7 实现，Task 9 集成，Task 10 检测 |
| §3.1 FR-1 搜索采集 | Task 8, 9 | Bilibili 搜索 + RunStage0 |
| §3.1 FR-2 博主聚合 | Task 5, 8, 9 | 统计 + Bilibili 详情 + RunStage1 |
| §3.1 FR-3 命令行参数 | Task 10 | main.go CLI 解析 |
| §3.1 FR-4 CSV 输出 | Task 6 | export/ |
| §3.2 NFR-1 断点续爬 | Task 7, 9, 10 | progress/ + 编排集成 + 启动检测 |
| §3.2 NFR-2 并发控制 | Task 4, 9 | pool/ + 编排集成 |
| §3.2 NFR-3 错误处理 | Task 3, 4, 9 | 三层错误处理 |
| §3.2 NFR-4 可扩展性 | Task 1, 8 | 接口抽象 + 平台隔离 |
| §8.1 可观测性 | Task 3, 9, 10 | 日志输出分布在各层 |
| §8.2 配置参数 | Task 2 | config/ |
