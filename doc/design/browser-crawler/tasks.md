# 实施任务清单

> 由 spec.md 生成
> 任务总数: 8
> 核心原则: 先建基础设施（browser 包），再建平台实现（platform/bilibili），最后接入入口（cmd/crawler）——自底向上，每步可编译

## 依赖关系总览

```
Task 1 (go.mod 添加 rod 依赖)
  ↓
Task 2 (config 修改：新增 BrowserConfig，移除 HTTPConfig)
  ↓
Task 3 (browser/browser.go — 浏览器生命周期管理)  ← 依赖 Task 1
  ↓
Task 4 (browser/interceptor.go — 通用网络请求拦截)  ← 依赖 Task 3
  ↓
Task 5 (browser/auth.go — 登录态管理)  ← 依赖 Task 3
  ↓
Task 6 (platform/bilibili/types.go — API 响应类型迁移)  ← 无代码依赖，但逻辑上在 Task 7 之前
  ↓
Task 7 (platform/bilibili/search.go + author.go — 平台实现)  ← 依赖 Task 3, 4, 6
  ↓
Task 8 (cmd/crawler/main.go + conf/config.yaml.example — 入口程序)  ← 依赖 Task 2, 3, 5, 7
```

## 变更影响概览

### 文件变更清单

| 文件 | 操作 | 涉及任务 | 说明 |
|------|------|---------|------|
| `go.mod` / `go.sum` | 修改 | Task 1 | 添加 `github.com/go-rod/rod` 依赖 |
| `src/config/config.go` | 修改 | Task 2 | 新增 `BrowserConfig`，移除 `HTTPConfig`，调整默认值 |
| `src/browser/browser.go` | 新建 | Task 3 | 浏览器生命周期管理：Manager、Page 池 |
| `src/browser/interceptor.go` | 新建 | Task 4 | 通用网络请求拦截：NavigateAndIntercept、WaitForIntercept |
| `src/browser/auth.go` | 新建 | Task 5 | 登录态管理：EnsureLogin、InjectCookie、WaitForManualLogin |
| `src/platform/bilibili/types.go` | 新建 | Task 6 | 从 deprecated 迁移 API 响应类型定义 |
| `src/platform/bilibili/search.go` | 新建 | Task 7 | BiliBrowserSearchCrawler 实现 |
| `src/platform/bilibili/author.go` | 新建 | Task 7 | BiliBrowserAuthorCrawler 实现 |
| `cmd/crawler/main.go` | 新建 | Task 8 | 新入口程序 |
| `conf/config.yaml.example` | 修改 | Task 8 | 更新配置示例（移除 http 块，新增 browser 块） |

## 任务列表

### 任务 1: [x] 添加 Rod 依赖

- 文件: `go.mod`（修改）, `go.sum`（自动更新）
- 依赖: 无
- spec 映射: §4.1 方案概览（Rod 选型）
- 说明: 在 go.mod 中添加 `github.com/go-rod/rod` 依赖。这是所有浏览器相关代码的基础依赖。
- context:
  - `go.mod` — 直接修改目标
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `go mod tidy` 无报错
- 子任务:
  - [ ] 1.1: 执行 `go get github.com/go-rod/rod`
  - [ ] 1.2: 确认 go.mod 中出现 rod 依赖

### 任务 2: [x] 修改配置模块 — 新增 BrowserConfig，移除 HTTPConfig

- 文件: `src/config/config.go`（修改）
- 依赖: 无
- spec 映射: §4.2.1 config.go 变更, §3.1 FR-1
- 说明: 在 Config 结构体中新增 `BrowserConfig`（headless、user_data_dir），移除 `HTTPConfig` 及其相关默认值和 applyDefaults 逻辑。保留 Cookie 字段（用于 headless 降级注入）。
- context:
  - `src/config/config.go` — 直接修改目标
  - `deprecated/api/crawler/main.go` — 旧入口引用了 `cfg.HTTP`，但已废弃不影响
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `Config` 结构体包含 `Browser BrowserConfig` 字段
  - [ ] `HTTPConfig` 类型和相关常量/默认值已移除
  - [ ] `BrowserConfig` 有合理的默认值（headless=true, user_data_dir="data/browser-profile/"）
- 子任务:
  - [ ] 2.1: 定义 `BrowserConfig` 结构体
  - [ ] 2.2: 修改 `Config` 结构体：替换 `HTTP HTTPConfig` 为 `Browser BrowserConfig`
  - [ ] 2.3: 移除 HTTP 相关常量（DefaultHTTPTimeout 等）
  - [ ] 2.4: 更新 `applyDefaults` — 移除 HTTP 默认值，新增 Browser 默认值
  - [ ] 2.5: 更新 `RequestInterval` 默认值为 500ms（浏览器方案下可设较小值）

### 任务 3: [x] 实现浏览器生命周期管理 — browser.go

- 文件: `src/browser/browser.go`（新建）
- 依赖: Task 1
- spec 映射: §4.2.1 browser.go, §4.3.1 浏览器生命周期管理
- 说明: 实现 Manager 结构体，管理 Chromium Browser 进程的启动/关闭和 Page 池的创建/借用/归还。使用 buffered channel 作为 Page 池，PutPage 时导航到 about:blank 重置状态。
- context:
  - `src/browser/browser.go` — 新建文件
  - `src/config/config.go:BrowserConfig` — 配置来源
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `Manager` 导出 `New()`, `GetPage()`, `PutPage()`, `Close()`, `Browser()` 方法
  - [ ] `Config` 结构体包含 Headless、UserDataDir、Concurrency 字段
  - [ ] `buildLauncher` 设置了 headless、user-data-dir、disable-gpu、no-sandbox 等参数
- 子任务:
  - [ ] 3.1: 创建 `src/browser/` 目录和 `browser.go` 文件
  - [ ] 3.2: 定义 `Config` 和 `Manager` 结构体
  - [ ] 3.3: 实现 `buildLauncher()` — 构建 Launcher 参数
  - [ ] 3.4: 实现 `New()` — 启动 Browser、创建 Page 池
  - [ ] 3.5: 实现 `GetPage()` / `PutPage()` — Page 池借用/归还
  - [ ] 3.6: 实现 `Close()` — 优雅关闭
  - [ ] 3.7: 实现 `Browser()` — 暴露底层 Browser

### 任务 4: [x] 实现通用网络请求拦截 — interceptor.go

- 文件: `src/browser/interceptor.go`（新建）
- 依赖: Task 3
- spec 映射: §4.2.1 interceptor.go, §4.3.2 网络请求拦截
- 说明: 实现基于 CDP `proto.NetworkResponseReceived` 事件的被动网络响应监听。提供两个核心函数：`NavigateAndIntercept`（导航+拦截）和 `WaitForIntercept`（已加载页面的拦截等待）。URL 匹配使用子串匹配。
- context:
  - `src/browser/interceptor.go` — 新建文件
  - `src/browser/browser.go:Manager` — 提供 Page
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `InterceptRule` 和 `InterceptResult` 类型已导出
  - [ ] `NavigateAndIntercept` 函数签名与 spec §4.2.1 一致
  - [ ] `WaitForIntercept` 函数签名与 spec §4.2.1 一致
  - [ ] 使用 `proto.NetworkResponseReceived` 事件（非 HijackRequests）
- 子任务:
  - [ ] 4.1: 定义 `InterceptRule` 和 `InterceptResult` 类型
  - [ ] 4.2: 实现 `NavigateAndIntercept` — 设置事件监听 → 导航 → 等待匹配/超时
  - [ ] 4.3: 实现 `WaitForIntercept` — 设置事件监听 → 返回 channel + cancel
  - [ ] 4.4: 实现 `getResponseBody` 辅助函数 — 通过 CDP 获取 response body

### 任务 5: [x] 实现登录态管理 — auth.go

- 文件: `src/browser/auth.go`（新建）
- 依赖: Task 3
- spec 映射: §4.2.1 auth.go, §4.3.3 登录态管理, §3.1 FR-3
- 说明: 实现 EnsureLogin 策略（user_data_dir → 手动登录 → Cookie 注入 → 匿名），InjectCookie（解析 cookie 字符串并通过 CDP 注入），WaitForManualLogin（轮询 checker 等待手动登录完成）。LoginChecker 是函数类型，由平台层提供具体实现。
- context:
  - `src/browser/auth.go` — 新建文件
  - `src/browser/browser.go:Manager` — 提供 Browser 和 Page
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `LoginChecker` 函数类型已导出
  - [ ] `EnsureLogin` 实现了 spec §4.3.3 的完整决策流程
  - [ ] `InjectCookie` 能解析 "key1=value1; key2=value2" 格式
  - [ ] `WaitForManualLogin` 使用轮询机制（2-3s 间隔）
- 子任务:
  - [ ] 5.1: 定义 `LoginChecker` 函数类型
  - [ ] 5.2: 实现 `InjectCookie` — 解析 cookie 字符串 + CDP 注入
  - [ ] 5.3: 实现 `WaitForManualLogin` — 轮询 checker
  - [ ] 5.4: 实现 `EnsureLogin` — 完整策略分支

### 任务 6: [x] 迁移 Bilibili API 响应类型 — types.go

- 文件: `src/platform/bilibili/types.go`（新建）
- 依赖: 无（纯类型定义）
- spec 映射: §4.2.3 数据模型（迁移的类型）
- 说明: 从 `deprecated/api/bilibili/types.go` 复制 API 响应类型定义（SearchResp、UserInfoResp、UserStatResp、VideoListResp 及其子类型）。不复制 `apiError` 和 `retryableAPICodes`（浏览器方案不需要 API 级重试）。同时新增 `videoPageSize` 常量和 `VideoPageSize()` 函数，以及通用的辅助函数（stripHTMLTags、parseDuration）。
- context:
  - `src/platform/bilibili/types.go` — 新建文件
  - `deprecated/api/bilibili/types.go` — 复制来源
  - `deprecated/api/bilibili/search.go` — stripHTMLTags、parseDuration 来源
  - `deprecated/api/bilibili/author.go` — parseVideoDuration 来源
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] 包含 SearchResp、UserInfoResp、UserStatResp、VideoListResp 等类型
  - [ ] 包含 stripHTMLTags、parseDuration 辅助函数
  - [ ] 包含 `videoPageSize` 常量和 `VideoPageSize()` 函数
  - [ ] 不包含 apiError、retryableAPICodes
- 子任务:
  - [ ] 6.1: 创建 `src/platform/bilibili/` 目录和 `types.go` 文件
  - [ ] 6.2: 复制 API 响应类型定义（SearchResp 系列、UserInfoResp 系列、VideoListResp 系列）
  - [ ] 6.3: 复制并统一 parseDuration / stripHTMLTags 辅助函数
  - [ ] 6.4: 添加 videoPageSize 常量和 VideoPageSize() 函数

### 任务 7: [x] 实现 Bilibili 平台浏览器爬虫 — search.go + author.go

- 文件: `src/platform/bilibili/search.go`（新建）, `src/platform/bilibili/author.go`（新建）
- 依赖: Task 3, 4, 6
- spec 映射: §4.2.1 platform/bilibili, §4.3.4 搜索爬虫, §4.3.5 博主爬虫, §3.1 FR-4/FR-5
- 说明: 实现 `BiliBrowserSearchCrawler`（SearchCrawler 接口）和 `BiliBrowserAuthorCrawler`（AuthorCrawler 接口）。每个方法内部从 Manager 借用 Page → 构造 URL → NavigateAndIntercept → 解析 JSON → 转换为通用类型 → 归还 Page。同时实现 `BilibiliLoginChecker` 函数（检查 SESSDATA cookie）。
- context:
  - `src/platform/bilibili/search.go` — 新建
  - `src/platform/bilibili/author.go` — 新建
  - `src/browser/browser.go:Manager` — GetPage/PutPage
  - `src/browser/interceptor.go` — NavigateAndIntercept
  - `src/platform/bilibili/types.go` — API 响应类型
  - `src/types.go` — Video、AuthorInfo、VideoDetail、PageInfo
  - `src/crawler.go` — SearchCrawler、AuthorCrawler 接口定义
  - `deprecated/api/bilibili/search.go` — 数据转换逻辑参考
  - `deprecated/api/bilibili/author.go` — 数据转换逻辑参考
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `BiliBrowserSearchCrawler` 实现 `src.SearchCrawler` 接口（编译时检查）
  - [ ] `BiliBrowserAuthorCrawler` 实现 `src.AuthorCrawler` 接口（编译时检查）
  - [ ] `BilibiliLoginChecker` 函数签名匹配 `browser.LoginChecker` 类型
  - [ ] SearchPage 拦截 `/x/web-interface/search/type` URL
  - [ ] FetchAuthorInfo 一次导航拦截两个 API（user_info + user_stat）
  - [ ] FetchAuthorVideos 拦截 `/x/space/wbi/arc/search` URL
- 子任务:
  - [ ] 7.1: 实现 `BiliBrowserSearchCrawler` 和 `NewSearchCrawler`
  - [ ] 7.2: 实现 `SearchPage` — URL 构造 + 拦截 + JSON 解析 + 类型转换
  - [ ] 7.3: 实现 `BiliBrowserAuthorCrawler` 和 `NewAuthorCrawler`
  - [ ] 7.4: 实现 `FetchAuthorInfo` — 双 API 拦截 + 解析
  - [ ] 7.5: 实现 `FetchAuthorVideos` — URL 翻页 + 拦截 + 解析
  - [ ] 7.6: 实现 `BilibiliLoginChecker` — 检查 SESSDATA cookie

### 任务 8: [x] 实现入口程序 + 更新配置示例

- 文件: `cmd/crawler/main.go`（新建）, `conf/config.yaml.example`（修改）
- 依赖: Task 2, 3, 5, 7
- spec 映射: §4.3.6 入口程序, §4.3.7 配置文件示例, §3.1 FR-6
- 说明: 新建 `cmd/crawler/main.go`，初始化浏览器 Manager → EnsureLogin → 创建平台实现 → 注入编排层运行。参考 `deprecated/api/crawler/main.go` 的模式（flag 解析、signal handling、progress 管理、adaptPoolRun）。更新 `conf/config.yaml.example` 移除 http 块，新增 browser 块。
- context:
  - `cmd/crawler/main.go` — 新建
  - `conf/config.yaml.example` — 修改
  - `deprecated/api/crawler/main.go` — 参考模式
  - `src/browser/browser.go:Manager` — 浏览器管理
  - `src/browser/auth.go:EnsureLogin` — 登录态
  - `src/platform/bilibili/search.go` — SearchCrawler 实现
  - `src/platform/bilibili/author.go` — AuthorCrawler 实现
  - `src/crawler.go:RunStage0/RunStage1` — 编排层
- 验收标准:
  - [ ] `go build ./cmd/crawler/...` 编译通过
  - [ ] `go build ./...` 编译通过
  - [ ] main.go 包含 flag 解析（keyword, platform, stage, config）
  - [ ] main.go 包含 signal handling（SIGINT/SIGTERM → graceful shutdown）
  - [ ] main.go 包含 progress 管理（断点续采）
  - [ ] main.go 调用 `browser.New()` → `browser.EnsureLogin()` → 创建 crawler → RunStage0/RunStage1
  - [ ] `conf/config.yaml.example` 包含 browser 块，不包含 http 块
- 子任务:
  - [ ] 8.1: 创建 `cmd/crawler/` 目录和 `main.go` 文件
  - [ ] 8.2: 实现 flag 解析和参数校验
  - [ ] 8.3: 实现配置加载和 signal handling
  - [ ] 8.4: 实现浏览器初始化（Manager + EnsureLogin）
  - [ ] 8.5: 实现 Stage 0/1 执行逻辑（复用 deprecated main.go 的 adaptPoolRun 模式）
  - [ ] 8.6: 更新 `conf/config.yaml.example`

---

## Spec 覆盖映射

| Spec 章节 | 任务 | 说明 |
|-----------|------|------|
| §3.1 FR-1 浏览器配置 | Task 2 | BrowserConfig 新增 |
| §3.1 FR-2 浏览器生命周期管理 | Task 3 | Manager 实现 |
| §3.1 FR-3 登录态管理 | Task 5, 7 | auth.go + BilibiliLoginChecker |
| §3.1 FR-4 网络请求拦截 | Task 4 | interceptor.go |
| §3.1 FR-5 Bilibili 浏览器实现 | Task 6, 7 | types.go + search.go + author.go |
| §3.1 FR-6 新入口 | Task 8 | cmd/crawler/main.go |
| §4.1 方案概览 | Task 1, 3 | Rod 依赖 + Manager |
| §4.2.1 核心模块设计 | Task 3, 4, 5 | browser 包三个文件 |
| §4.2.2 接口设计 | Task 7 | 接口实现 + 编译时检查 |
| §4.2.3 数据模型 | Task 6 | 类型迁移 |
| §4.2.4 并发模型 | Task 3, 7 | Page 池 + GetPage/PutPage |
| §4.2.5 错误处理 | Task 3, 4, 7 | 各模块错误处理 |
| §4.3.1 浏览器生命周期 | Task 3 | New/Close/GetPage/PutPage |
| §4.3.2 网络请求拦截 | Task 4 | NavigateAndIntercept/WaitForIntercept |
| §4.3.3 登录态管理 | Task 5 | EnsureLogin 决策流程 |
| §4.3.4 搜索爬虫 | Task 7 | SearchPage 实现 |
| §4.3.5 博主爬虫 | Task 7 | FetchAuthorInfo/FetchAuthorVideos |
| §4.3.6 入口程序 | Task 8 | cmd/crawler/main.go |
| §4.3.7 配置文件示例 | Task 8 | config.yaml.example |
| §8.1 日志 | Task 3, 4, 5, 7, 8 | 各模块 log.Printf 输出 |
