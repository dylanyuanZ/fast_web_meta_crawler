# Feature: 浏览器自动化爬虫方案（替代 API 直调）

**作者**: AI + User  
**日期**: 2026-03-27  
**状态**: Draft

---

## 1. 背景 (Background)

### 1.1 问题描述

当前项目通过 HTTP API 直调方式采集 Bilibili 平台数据。在小规模任务（~20 个博主）下表现良好（100% 成功率），但在大规模任务下遇到严重的反爬瓶颈：

**实际案例**：搜索"生化危机9"，Stage 0 发现 1000 条视频、619 个独立作者。Stage 0 在 2m13s 内顺利完成，但 Stage 1（博主详情采集）跑了 20 分钟后，所有请求全部返回风控校验失败（`-352`），指数退避重试也无法恢复。

**根因分析**：B 站存在 IP 级别的**累计阈值**机制——短时间内 API 调用次数超过某个上限后，Cookie/IP 被标记，后续请求**全部拒绝**，不是"等一等就好"的频率限制，而是"封死"。

### 1.2 现状分析

| 维度 | 现状 |
|------|------|
| **Stage 0（搜索）** | API 直调，表现良好，50 页搜索 ~2 分钟完成，无风控问题 |
| **Stage 1（博主详情）** | API 直调，小规模可用（20 博主 100% 成功），大规模（600+ 博主）触发累计风控阈值后完全失败 |
| **反爬策略** | Cookie 预热 + wbi 签名 + 指数退避重试 + 请求间隔随机抖动，已达到 API 方案的极限 |
| **架构** | `SearchCrawler` / `AuthorCrawler` 接口已解耦，编排层不关心底层实现方式，具备扩展空间 |
| **技术选型** | 技术方案分析中已选定 **Rod** 作为浏览器自动化库（API 友好，自动管理浏览器生命周期，支持自动下载 Chromium） |

### 1.4 决策：废弃 API 方案

经评估，API 直调方案在大规模场景下**不可靠**，且无法通过调参解决（根因是平台累计阈值机制）。决定：

- **废弃 API 直调方案**，不再维护双模式切换
- 原 API 相关代码（`httpclient/`、`platform/bilibili/`、`cmd/crawler/`、`cmd/benchmark/`）已移至 `deprecated/api/` 目录，仅供参考
- **浏览器自动化方案作为唯一的数据采集方式**
- 保留通用模块：编排层（`crawler.go`）、数据类型（`types.go`）、配置（`config/`）、导出（`export/`）、并发池（`pool/`）、进度管理（`progress/`）、统计（`stats/`）

### 1.3 主要使用场景

1. **大规模 Bilibili 博主采集**：搜索热门关键词（如"生化危机9"），发现数百个博主，需要逐个采集详情。API 方案在此场景下触发累计风控，无法完成任务
2. **未来接入其他视频平台**：YouTube 等平台可能没有公开 API，或 API 反爬更严格，浏览器方案是通用的兜底策略
3. **服务器部署**：工具需要在 Linux 服务器（无 GUI，最多 2C4G）上运行，浏览器方案需要支持 headless 模式
4. **本地开发/调试**：本地有 GUI 环境，可以打开可见浏览器进行手动登录

## 2. 目标 (Goals)

1. **浏览器自动化作为唯一采集方式**：基于 Rod 实现 `SearchCrawler` 和 `AuthorCrawler` 接口的浏览器版本，完全替代已废弃的 API 直调方案
2. **网络请求拦截**：浏览器方案通过拦截浏览器发出的 API 请求获取结构化 JSON 数据（而非 DOM 解析），数据格式与原 API 方案一致
3. **登录态管理**：支持手动登录 + user-data-dir 持久化，解决 Cookie 过期问题；服务器无 GUI 环境降级到 Cookie 注入
4. **并发控制**：复用现有 `concurrency` 配置，作为浏览器实例数（推荐 3-5 个）
5. **资源可配**：浏览器实例数、headless 模式等参数可配置，适配服务器（2C4G）和本地不同环境
6. **通用架构**：浏览器方案的设计应具备通用性，未来接入其他平台时可复用浏览器管理、登录态管理等基础能力
7. **新入口**：新建 `cmd/crawler/main.go`，初始化浏览器方案并注入到编排层

### 2.1 非目标 (Non-Goals)

1. **不做代理 IP 池**：代理 IP 是另一个独立的优化方向，不在本次范围内
2. **不做反指纹检测**：不主动对抗 headless 浏览器指纹检测（如 Canvas/WebGL 伪装），Rod 默认的反检测能力已足够
3. **不恢复 API 方案**：API 直调方案已废弃，不维护、不提供切换选项
4. **不做远程登录**：服务器无 GUI 时不提供 VNC/端口转发等远程登录方案，降级到 Cookie 注入即可

## 3. 需求细化 (Requirements)

### 3.1 功能性需求

#### FR-1: 浏览器配置

- 移除 `mode` 字段（不再需要 API/浏览器切换）
- 在 `config.yaml` 中新增 `browser` 配置块，包含：
  - `headless`: bool，是否无头模式（默认 `true`，服务器环境必须为 `true`）
  - `user_data_dir`: string，浏览器用户数据目录路径（用于持久化登录态，默认 `data/browser-profile/`）
- 移除 `config.yaml` 中 API 专属的配置项（如 `http` 块中的 `timeout`、`max_retries` 等），保留通用配置

#### FR-2: 浏览器生命周期管理

- 启动时根据 `concurrency` 配置创建对应数量的浏览器实例（共享同一个 Browser 进程的多个 Page，或多个独立 Browser 进程，由设计阶段决定）
- 任务完成后优雅关闭所有浏览器实例，释放资源
- 异常退出时确保浏览器进程不残留

#### FR-3: 登录态管理

- **有 GUI 环境（headless=false）**：首次运行时打开可见浏览器，用户手动登录目标平台，登录成功后浏览器自动将 Cookie/Session 保存到 `user_data_dir`
- **无 GUI 环境（headless=true）**：
  - 优先检查 `user_data_dir` 中是否有之前保存的登录态
  - 如果没有或已过期，降级到 `config.yaml` 中的 `cookie` 字段注入
  - 如果 `cookie` 也未配置，以匿名模式运行
- 登录态检测：打开平台页面后，通过检查特定元素或 Cookie 判断是否已登录

#### FR-4: 网络请求拦截获取数据

- 浏览器打开目标页面后，通过 Rod 的 `HijackRequests` 或事件监听机制，拦截页面 JS 发出的 API 请求
- 从拦截到的 Response 中提取 JSON 数据，解析为与 API 方案相同的数据结构（`Video`、`AuthorInfo`、`VideoDetail` 等）
- 对于需要翻页的场景（搜索结果、博主视频列表），通过模拟页面操作（滚动、点击"下一页"）触发浏览器发出后续 API 请求

#### FR-5: Bilibili 浏览器实现

- 实现 `BiliBrowserSearchCrawler`（实现 `SearchCrawler` 接口）：
  - 打开 B 站搜索页面 `https://search.bilibili.com/video?keyword=xxx`
  - 拦截搜索 API 响应，解析为 `[]Video` + `PageInfo`
  - 通过模拟翻页操作获取后续页面数据
- 实现 `BiliBrowserAuthorCrawler`（实现 `AuthorCrawler` 接口）：
  - `FetchAuthorInfo`：打开博主主页 `https://space.bilibili.com/{mid}`，拦截用户信息和粉丝数 API 响应
  - `FetchAuthorVideos`：在博主主页的投稿视频 Tab，拦截视频列表 API 响应，模拟翻页获取后续数据

#### FR-6: 新入口

- 新建 `cmd/crawler/main.go`，直接创建浏览器实现并注入到 `RunStage0` / `RunStage1`
- 编排层（`crawler.go`）无需修改，通过接口多态自动适配
- 移除 API 方案相关的初始化代码（`httpclient.New()`、`bilibili.NewSearchCrawler()` 等）

### 3.2 非功能性需求

#### NFR-1: 资源控制

- 单个浏览器实例内存占用控制在 ~200-400MB
- `concurrency` 在浏览器模式下推荐值 3-5，最大值仍为 16（但需在文档中提示资源消耗）
- 服务器 2C4G 环境下，推荐 `concurrency: 2`，headless 模式

#### NFR-2: 稳定性

- 浏览器实例崩溃时能自动恢复（重启实例），不影响其他实例
- 页面加载超时时有合理的重试机制
- 任务完成后无浏览器进程残留

#### NFR-3: 可观测性

- 浏览器方案的日志应包含：实例 ID、页面 URL、拦截到的 API 数量、耗时等关键信息
- 复用现有 progress 机制保持进度报告格式

#### NFR-4: 跨平台通用性

- 浏览器管理（启动、关闭、实例池）、登录态管理（user-data-dir、Cookie 注入）等基础能力应设计为平台无关的通用模块
- Bilibili 特定的页面操作（URL 构造、翻页方式、API 拦截规则）封装在 `platform/bilibili/` 下

## 4. 设计方案 (Design)

### 4.1 方案概览

#### 核心思路

用 Rod 驱动一个 Chromium 浏览器进程，通过多个 Page（Tab）并发访问目标平台页面，利用网络请求拦截（而非 DOM 解析）获取页面 JS 发出的 API 响应 JSON，解析为与原 API 方案一致的数据结构，注入到现有编排层（`crawler.go`）中运行。

#### 模块划分

```
src/
├── browser/                    # 浏览器基础设施层（平台无关）
│   ├── browser.go              # 浏览器生命周期管理：启动 Browser、创建/回收 Page 池
│   ├── interceptor.go          # 通用网络请求拦截框架：按 URL pattern 匹配、提取 response body
│   └── auth.go                 # 登录态管理：user-data-dir 持久化、Cookie 注入、登录检测
│
├── platform/
│   └── bilibili/               # Bilibili 平台实现层
│       ├── search.go           # BiliBrowserSearchCrawler（实现 SearchCrawler 接口）
│       ├── author.go           # BiliBrowserAuthorCrawler（实现 AuthorCrawler 接口）
│       └── types.go            # Bilibili API 响应类型定义（从 deprecated 迁移）
│
├── crawler.go                  # 编排层（不变）
├── types.go                    # 通用数据类型（不变）
├── config/config.go            # 配置（修改：新增 BrowserConfig，移除 HTTPConfig）
├── pool/pool.go                # 并发池（不变）
├── progress/progress.go        # 进度管理（不变）
├── stats/stats.go              # 统计计算（不变）
└── export/export.go            # CSV 导出（不变）

cmd/
└── crawler/main.go             # 新入口（新增）

conf/
└── config.yaml.example         # 配置示例（修改）
```

#### 依赖方向

```
cmd/crawler/main.go          ← 应用入口
    │
    ├──→ src/config           ← 配置加载
    ├──→ src/browser          ← 浏览器基础设施（启动 Browser、Page 池、登录态）
    ├──→ src/platform/bilibili ← 平台实现（依赖 browser 包）
    └──→ src/crawler.go       ← 编排层（依赖 SearchCrawler/AuthorCrawler 接口）
              │
              └──→ src/pool, src/progress, src/stats, src/export
```

**依赖规则**：
- `src/browser` 是基础设施层，不依赖任何业务模块
- `src/platform/bilibili` 是应用层，依赖 `src/browser` 获取浏览器能力
- `src/crawler.go`（编排层）只依赖 `SearchCrawler` / `AuthorCrawler` 接口，不感知浏览器实现
- 禁止循环依赖：`browser` ← `platform/bilibili` ← `cmd/crawler`，单向链

#### 数据流

```
Stage 0（搜索）:
  cmd/main.go
    → RunStage0(ctx, searchCrawler, keyword, cfg)
      → searchCrawler.SearchPage(ctx, keyword, page)
        → [browser] 打开 B 站搜索页 URL
        → [browser] 拦截 api.bilibili.com/x/web-interface/search/type 响应
        → [platform] 解析 JSON → []Video + PageInfo
      → 编排层并发调度多页（pool.Run）
      → 去重 → 写 video CSV + intermediate JSON

Stage 1（博主详情）:
  cmd/main.go
    → RunStage1(ctx, authorCrawler, mids, cfg)
      → authorCrawler.FetchAuthorInfo(ctx, mid)
        → [browser] 打开博主主页 URL
        → [browser] 拦截 user info + follower stat API 响应
        → [platform] 解析 JSON → AuthorInfo
      → authorCrawler.FetchAuthorVideos(ctx, mid, page)
        → [browser] 在博主主页投稿 Tab 拦截视频列表 API 响应
        → [platform] 解析 JSON → []VideoDetail + PageInfo
      → 编排层并发调度多博主（pool.Run）
      → 计算统计 → 写 author CSV
```

#### 浏览器实例模型

采用**单 Browser 进程 + 多 Page（Tab）**模型：

- 启动时创建 1 个 Chromium Browser 进程，共享 `user_data_dir`（登录态）
- 创建 `concurrency` 个 Page 作为并发工作单元，由 Page 池管理
- 每个 worker goroutine 从 Page 池借用一个 Page，完成任务后归还
- Page 崩溃时自动创建新 Page 替换，不影响其他 Page

**选择理由**：
- 资源友好：单进程 + N 个 Tab，比 N 个独立进程节省大量内存，适配 2C4G 服务器
- 登录态共享：所有 Tab 共享同一个 Browser 的 Cookie/Session，无需多次登录
- 隔离性足够：Page 级别的崩溃不会导致 Browser 进程退出，Rod 支持 Page 级错误恢复

**Trade-off**：
- 牺牲了进程级隔离（一个 Browser 崩溃影响所有 Tab），但 Chromium 的多进程架构已在内部提供了 Tab 级隔离
- 所有 Tab 共享同一 IP + Cookie，无法通过多实例分散风控压力——但这不是本方案的目标（非目标中已排除代理 IP 池）

### 4.2 组件设计 (Component Design)

#### 4.2.1 核心模块设计

##### src/browser/browser.go — 浏览器生命周期管理

**职责**：管理 Chromium Browser 进程的启动/关闭，以及 Page 池的创建/借用/归还。

```go
package browser

import "github.com/go-rod/rod"

// Manager manages a single Browser process and a pool of reusable Pages.
// It is the central entry point for all browser operations.
// Safe for concurrent use.
type Manager struct {
    browser     *rod.Browser
    pagePool    chan *rod.Page  // buffered channel as Page pool
    cfg         Config
}

// Config holds browser-related configuration (from config.yaml browser block).
type Config struct {
    Headless    bool   // headless mode (default true)
    UserDataDir string // user data directory for login persistence
    Concurrency int    // number of Pages to create (= pool size)
}

// New creates a Manager, launches a Chromium process, and pre-creates Pages.
// The Browser uses cfg.UserDataDir for login state persistence.
func New(cfg Config) (*Manager, error)

// GetPage borrows a Page from the pool (blocks if all Pages are in use).
// The caller MUST call PutPage() when done.
func (m *Manager) GetPage() *rod.Page

// PutPage returns a Page to the pool after use.
// Navigates the Page to about:blank to reset state before returning.
func (m *Manager) PutPage(page *rod.Page)

// Close gracefully shuts down all Pages and the Browser process.
// Must be called on program exit (typically via defer).
func (m *Manager) Close() error

// Browser returns the underlying rod.Browser for advanced operations.
// Used by auth module for login flow.
func (m *Manager) Browser() *rod.Browser
```

**关键设计决策**：

| 决策 | 理由 |
|------|------|
| Page 池用 buffered channel 实现 | Go 惯用模式，天然支持阻塞等待和并发安全，无需额外锁 |
| `PutPage` 时导航到 `about:blank` | 重置页面状态（清除 JS 上下文、停止网络请求），避免 Page 复用时残留上一个任务的拦截器 |
| `Manager` 暴露 `Browser()` 方法 | 登录态管理需要直接操作 Browser（如打开新 Page 进行手动登录），但日常使用只通过 `GetPage/PutPage` |
| 不做 Page 崩溃自动恢复 | Rod 的 Page 崩溃概率极低（Chromium 内部已有 Tab 级进程隔离）；如果确实崩溃，worker 返回 error，编排层的 Pool 会记录失败，不影响其他 worker |

##### src/browser/interceptor.go — 通用网络请求拦截框架

**职责**：提供通用的网络请求拦截能力，按 URL pattern 匹配目标 API 请求，提取 response body。

```go
package browser

import (
    "context"
    "github.com/go-rod/rod"
)

// InterceptRule defines a single URL pattern to intercept and how to handle it.
type InterceptRule struct {
    // URLPattern is a substring match against the request URL.
    // Example: "/x/web-interface/search/type" matches Bilibili search API.
    URLPattern string

    // ID is a unique identifier for this rule, used to retrieve the captured response.
    // Example: "search", "user_info", "user_stat"
    ID string
}

// InterceptResult holds the captured response for a matched rule.
type InterceptResult struct {
    ID   string // matches InterceptRule.ID
    Body []byte // raw response body (JSON)
    URL  string // full request URL (for debugging)
}

// NavigateAndIntercept opens a URL in the given Page and waits for all specified
// rules to be matched (or context timeout/cancellation).
//
// Flow:
//  1. Set up network event listeners for the given rules
//  2. Navigate the Page to targetURL
//  3. Wait until all rules have captured a response, or ctx expires
//  4. Return captured results
//
// This is the primary function used by platform implementations.
func NavigateAndIntercept(ctx context.Context, page *rod.Page, targetURL string, rules []InterceptRule) ([]InterceptResult, error)

// WaitForIntercept waits for API responses matching the given rules on an already-loaded page.
// Used for pagination scenarios where the page is already open and we trigger
// a new API call via page interaction (e.g., clicking "next page").
//
// Flow:
//  1. Set up network event listeners for the given rules
//  2. Caller performs page interaction (click, scroll) AFTER calling this
//  3. Wait until all rules have captured a response, or ctx expires
//
// Returns a channel that will receive results as they arrive.
func WaitForIntercept(ctx context.Context, page *rod.Page, rules []InterceptRule) (<-chan InterceptResult, func(), error)
```

**关键设计决策**：

| 决策 | 理由 |
|------|------|
| 两个函数：`NavigateAndIntercept` + `WaitForIntercept` | 覆盖两种场景：首次加载页面（导航+拦截）和翻页（页面已加载，触发新请求+拦截） |
| URL 匹配用子串而非正则 | API URL 路径固定，子串匹配足够且性能更好；正则是过度设计 |
| `InterceptRule.ID` 标识规则 | 一次导航可能需要拦截多个 API（如博主主页同时拦截 user info + follower stat），用 ID 区分 |
| 基于 Rod 的 `proto.NetworkResponseReceived` 事件 | 不使用 `HijackRequests`（会修改请求流），而是被动监听网络事件，对页面行为零侵入 |

##### src/browser/auth.go — 登录态管理

**职责**：管理浏览器登录态的获取、持久化和检测。

```go
package browser

import (
    "context"
    "github.com/go-rod/rod"
)

// LoginChecker is a platform-specific function that checks if the browser is logged in.
// Returns true if logged in, false otherwise.
// Implementations check platform-specific cookies or page elements.
type LoginChecker func(ctx context.Context, page *rod.Page) (bool, error)

// EnsureLogin ensures the browser has a valid login state.
// Strategy (in priority order):
//  1. Check user_data_dir for existing login state (via checker)
//  2. If headless=false and not logged in: open login page, wait for manual login
//  3. If headless=true and not logged in: inject cookie from config
//  4. If no cookie configured: proceed anonymously (log warning)
//
// Parameters:
//   - manager: the browser Manager (provides Browser and Page access)
//   - loginURL: platform login page URL (e.g., "https://www.bilibili.com")
//   - cookie: cookie string from config (may be empty)
//   - checker: platform-specific login check function
func EnsureLogin(ctx context.Context, manager *Manager, loginURL string, cookie string, checker LoginChecker) error

// InjectCookie parses a cookie string and injects it into the browser.
// Cookie format: "key1=value1; key2=value2; ..."
func InjectCookie(page *rod.Page, domain string, cookieStr string) error

// WaitForManualLogin opens the login page in a visible browser and waits
// for the user to complete login manually.
// Polls the checker function until it returns true or ctx expires.
func WaitForManualLogin(ctx context.Context, page *rod.Page, loginURL string, checker LoginChecker) error
```

**关键设计决策**：

| 决策 | 理由 |
|------|------|
| `LoginChecker` 是函数类型而非接口 | 登录检测逻辑很简单（检查一个 Cookie 或元素），用函数类型比接口更轻量 |
| `EnsureLogin` 封装完整策略 | 调用方只需一行调用，不需要关心 headless/cookie/manual 的分支逻辑 |
| Cookie 注入通过 Rod 的 CDP 协议 | Rod 支持 `page.SetCookies()`，直接通过 Chrome DevTools Protocol 注入，比修改请求头更可靠 |
| 手动登录用轮询而非事件 | 登录成功的判断依赖 Cookie/元素检查，没有统一的"登录成功"事件可监听；轮询间隔 2-3 秒，开销可忽略 |

##### src/platform/bilibili/ — Bilibili 平台实现

**职责**：实现 `SearchCrawler` 和 `AuthorCrawler` 接口的浏览器版本，封装 Bilibili 特定的 URL 构造、拦截规则和 JSON 解析。

```go
package bilibili

import (
    "github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
)

// BiliBrowserSearchCrawler implements src.SearchCrawler using browser automation.
type BiliBrowserSearchCrawler struct {
    manager *browser.Manager
}

// NewSearchCrawler creates a new BiliBrowserSearchCrawler.
func NewSearchCrawler(manager *browser.Manager) *BiliBrowserSearchCrawler

// SearchPage opens Bilibili search page and intercepts the search API response.
// URL: https://search.bilibili.com/video?keyword={keyword}&page={page}
// Intercepts: api.bilibili.com/x/web-interface/search/type
func (c *BiliBrowserSearchCrawler) SearchPage(ctx context.Context, keyword string, page int) ([]src.Video, src.PageInfo, error)
```

```go
// BiliBrowserAuthorCrawler implements src.AuthorCrawler using browser automation.
type BiliBrowserAuthorCrawler struct {
    manager *browser.Manager
}

// NewAuthorCrawler creates a new BiliBrowserAuthorCrawler.
func NewAuthorCrawler(manager *browser.Manager) *BiliBrowserAuthorCrawler

// FetchAuthorInfo opens the author's space page and intercepts user info + stat APIs.
// URL: https://space.bilibili.com/{mid}
// Intercepts: api.bilibili.com/x/space/acc/info + api.bilibili.com/x/relation/stat
func (c *BiliBrowserAuthorCrawler) FetchAuthorInfo(ctx context.Context, mid string) (*src.AuthorInfo, error)

// FetchAuthorVideos opens the author's video tab and intercepts the video list API.
// URL: https://space.bilibili.com/{mid}/video
// Intercepts: api.bilibili.com/x/space/wbi/arc/search
func (c *BiliBrowserAuthorCrawler) FetchAuthorVideos(ctx context.Context, mid string, page int) ([]src.VideoDetail, src.PageInfo, error)
```

**关键设计决策**：

| 决策 | 理由 |
|------|------|
| 每次 `SearchPage`/`FetchAuthorInfo`/`FetchAuthorVideos` 调用都从 Page 池借用一个 Page | 与编排层的并发模型对齐：Pool 的每个 worker 调用一次接口方法，方法内部借用 Page、完成后归还 |
| `FetchAuthorInfo` 一次导航拦截两个 API（user info + stat） | 打开博主主页时，页面 JS 会同时请求这两个 API，一次导航即可拿到全部数据，无需两次导航 |
| `FetchAuthorVideos` 翻页通过 URL 参数而非模拟点击 | B 站博主视频列表支持 URL 参数翻页（`?pn=N`），直接导航到目标页比模拟点击更可靠、更快 |
| types.go 从 deprecated 迁移 | API 响应的 JSON 结构不变（浏览器拦截到的就是同一个 API 的响应），类型定义完全复用 |

##### src/config/config.go — 配置修改

**变更**：新增 `BrowserConfig`，移除 `HTTPConfig`。

```go
// BrowserConfig holds browser automation configuration.
type BrowserConfig struct {
    Headless    bool   `yaml:"headless"`      // headless mode (default true)
    UserDataDir string `yaml:"user_data_dir"` // browser profile directory
}

// Config holds all application configuration.
type Config struct {
    MaxSearchPage          int            `yaml:"max_search_page"`
    MaxVideoPerAuthor      int            `yaml:"max_video_per_author"`
    Concurrency            int            `yaml:"concurrency"`
    Browser                BrowserConfig  `yaml:"browser"`          // NEW
    MaxConsecutiveFailures int            `yaml:"max_consecutive_failures"`
    OutputDir              string         `yaml:"output_dir"`
    Cookie                 string         `yaml:"cookie"`
    RequestInterval        time.Duration  `yaml:"request_interval"`
    // HTTP HTTPConfig removed
}
```

##### cmd/crawler/main.go — 新入口

**职责**：初始化浏览器、登录态、平台实现，注入到编排层。

```go
func main() {
    // 1. Parse flags (keyword, platform, stage, config path)
    // 2. Load config
    // 3. Create browser.Manager
    // 4. EnsureLogin (with Bilibili-specific checker)
    // 5. Create BiliBrowserSearchCrawler + BiliBrowserAuthorCrawler
    // 6. Run stages (RunStage0 / RunStage1)
    // 7. defer manager.Close()
}
```

#### 4.2.2 接口设计

##### 接口不变（完全复用）

浏览器方案的核心优势：**编排层接口零修改**。

| 接口 | 方法 | 浏览器实现 | 所在文件 |
|------|------|-----------|---------|
| `SearchCrawler` | `SearchPage(ctx, keyword, page)` | `BiliBrowserSearchCrawler` | `platform/bilibili/search.go` |
| `AuthorCrawler` | `FetchAuthorInfo(ctx, mid)` | `BiliBrowserAuthorCrawler` | `platform/bilibili/author.go` |
| `AuthorCrawler` | `FetchAuthorVideos(ctx, mid, page)` | `BiliBrowserAuthorCrawler` | `platform/bilibili/author.go` |

编排层（`crawler.go`）、并发池（`pool/`）、进度管理（`progress/`）、统计（`stats/`）、导出（`export/`）**全部不变**。

##### 新增内部接口

| 接口/类型 | 包 | 用途 |
|----------|---|------|
| `browser.Manager` | `browser` | 浏览器生命周期 + Page 池管理 |
| `browser.InterceptRule` | `browser` | 定义 URL 拦截规则 |
| `browser.NavigateAndIntercept()` | `browser` | 导航+拦截一体化 |
| `browser.WaitForIntercept()` | `browser` | 已加载页面的拦截等待 |
| `browser.LoginChecker` | `browser` | 平台特定的登录检测函数 |
| `browser.EnsureLogin()` | `browser` | 登录态保障策略 |

#### 4.2.3 数据模型

##### 不变的类型（完全复用）

| 类型 | 所在文件 | 说明 |
|------|---------|------|
| `Video` | `src/types.go` | Stage 0 搜索结果 |
| `VideoDetail` | `src/types.go` | Stage 1 视频详情 |
| `AuthorInfo` | `src/types.go` | 博主基本信息 |
| `Author` | `src/types.go` | 博主聚合数据 |
| `AuthorStats` | `src/types.go` | 统计值 |
| `TopVideo` | `src/types.go` | TOP 视频 |
| `PageInfo` | `src/crawler.go` | 分页元数据 |
| `AuthorMid` | `src/crawler.go` | 博主 ID（中间数据） |

##### 迁移的类型（从 deprecated 复制）

| 类型 | 原位置 | 新位置 | 说明 |
|------|-------|-------|------|
| `SearchResp` / `SearchData` / `SearchItem` | `deprecated/api/bilibili/types.go` | `src/platform/bilibili/types.go` | 搜索 API JSON 结构 |
| `UserInfoResp` / `UserData` | 同上 | 同上 | 用户信息 API JSON 结构 |
| `UserStatResp` / `UserStatData` | 同上 | 同上 | 粉丝统计 API JSON 结构 |
| `VideoListResp` / `VideoListData` / `VideoListItems` / `VideoListItem` / `VideoListPage` | 同上 | 同上 | 视频列表 API JSON 结构 |

**说明**：浏览器拦截到的 API 响应 JSON 与 API 直调完全相同（同一个后端接口），因此类型定义可以直接复用。

##### 新增的类型

| 类型 | 所在文件 | 说明 |
|------|---------|------|
| `browser.Config` | `src/browser/browser.go` | 浏览器配置（从 config 传入） |
| `browser.InterceptRule` | `src/browser/interceptor.go` | URL 拦截规则 |
| `browser.InterceptResult` | `src/browser/interceptor.go` | 拦截结果 |
| `config.BrowserConfig` | `src/config/config.go` | YAML 配置映射 |

#### 4.2.4 并发模型

##### 线程模型

```
main goroutine
  │
  ├── browser.Manager (管理 1 个 Chromium 进程 + N 个 Page)
  │
  ├── RunStage0 → pool.Run(concurrency=N, tasks=pages)
  │     ├── worker-1: GetPage() → NavigateAndIntercept() → PutPage()
  │     ├── worker-2: GetPage() → NavigateAndIntercept() → PutPage()
  │     └── worker-N: GetPage() → NavigateAndIntercept() → PutPage()
  │
  └── RunStage1 → pool.Run(concurrency=N, tasks=mids)
        ├── worker-1: GetPage() → FetchAuthorInfo() → FetchAuthorVideos(page=1..M) → PutPage()
        ├── worker-2: ...
        └── worker-N: ...
```

##### 共享状态分析

| 共享资源 | 访问方式 | 保护机制 |
|---------|---------|---------|
| `Manager.pagePool` | 多个 worker goroutine 并发借用/归还 | buffered channel（天然并发安全） |
| `rod.Browser` | 多个 Page 共享同一 Browser 进程 | Rod 内部通过 CDP session 隔离，无需额外锁 |
| `rod.Page` | 每个 Page 同一时刻只被一个 worker 使用 | Page 池保证独占访问，无需锁 |
| 编排层的 `allVideos` / `results` | pool.Run 内部收集 | pool.Run 通过 channel 收集结果，无共享写入 |

##### 并发安全保证

- **无锁设计**：Page 池用 channel，结果收集用 channel，不引入 mutex
- **独占访问**：每个 Page 在借出期间只被一个 goroutine 使用
- **生命周期清晰**：Browser 在 main 中创建，defer Close()；Page 在 Manager 中创建，随 Browser 关闭

#### 4.2.5 错误处理

##### 失败模式

| 失败场景 | 错误类型 | 处理策略 |
|---------|---------|---------|
| Chromium 启动失败 | 致命错误 | 直接返回 error，程序退出 |
| Page 创建失败 | 致命错误 | 直接返回 error，程序退出 |
| 页面导航超时 | 可重试 | 返回 error 给 worker，由 pool 的熔断机制决定是否继续 |
| API 拦截超时（页面加载了但目标 API 未触发） | 可重试 | 设置拦截超时（如 30s），超时返回 error |
| API 响应 JSON 解析失败 | 不可重试 | 返回 error，记录原始 body 到日志 |
| API 响应 code != 0（如 -352 风控） | 取决于 code | 浏览器方案下风控概率极低，但仍返回 error 让 pool 处理 |
| Page 崩溃（Tab crash） | 可重试 | 返回 error，worker 下次调用时从 pool 获取新 Page |
| Browser 进程崩溃 | 致命错误 | 所有 worker 返回 error，pool 熔断，程序退出 |
| 登录态过期（中途） | 可重试 | 当前任务失败，后续任务可能也失败，触发 pool 熔断 |

##### 重试策略

浏览器方案的重试与 API 方案不同：

| 维度 | API 方案 | 浏览器方案 |
|------|---------|-----------|
| 重试层级 | `retryOnAPIError` 在 crawler 内部重试 | 不在 crawler 内部重试，依赖 pool 的熔断机制 |
| 重试原因 | API 返回 -799/-352 | 页面超时、拦截超时 |
| 退避策略 | 指数退避 | 无需退避（浏览器请求本身就慢，自带"退避"效果） |

**理由**：浏览器方案每次请求本身就包含页面加载（1-3 秒），天然比 API 直调慢得多，不需要额外的请求间隔和退避。如果连续失败，说明是系统性问题（如 IP 被封、浏览器崩溃），重试无意义，应该由 pool 的 `maxConsecutiveFailures` 熔断。

##### 资源清理

| 场景 | 清理动作 |
|------|---------|
| 正常退出 | `defer manager.Close()` → 关闭所有 Page → 关闭 Browser 进程 |
| panic/信号退出 | 注册 `os.Signal` handler，调用 `manager.Close()` |
| 单个 worker 失败 | Page 归还到池（`PutPage` 会 navigate to `about:blank` 重置状态） |

### 4.3 核心逻辑实现

#### 4.3.1 浏览器生命周期管理（browser.go）

##### 启动流程

```
New(cfg) →
  1. 构建 rod.Launcher 参数
     - 设置 Headless 模式
     - 设置 UserDataDir（登录态持久化）
     - 禁用不必要的功能（GPU、沙箱等，节省资源）
  2. launcher.Launch() → 获取 WebSocket URL
  3. rod.New().ControlURL(wsURL).Connect() → 连接 Browser
  4. 创建 buffered channel（容量 = Concurrency）
  5. 循环创建 Concurrency 个 Page，放入 channel
  6. 返回 Manager
```

##### Page 池借用/归还

```go
func (m *Manager) GetPage() *rod.Page {
    return <-m.pagePool  // 阻塞等待可用 Page
}

func (m *Manager) PutPage(page *rod.Page) {
    // 重置 Page 状态，避免残留上一个任务的拦截器和 JS 上下文
    page.MustNavigate("about:blank").MustWaitStable()
    m.pagePool <- page
}
```

##### 关闭流程

```
Close() →
  1. 排空 pagePool channel 中的所有 Page
  2. 逐个 page.Close()
  3. browser.Close() → 关闭 Chromium 进程
```

##### Launcher 参数配置

```go
func buildLauncher(cfg Config) *launcher.Launcher {
    l := launcher.New().
        Headless(cfg.Headless).
        UserDataDir(cfg.UserDataDir).
        Set("disable-gpu").
        Set("disable-dev-shm-usage").     // Docker/低内存环境必需
        Set("no-sandbox").                 // Linux 服务器环境必需
        Set("disable-background-networking").
        Set("disable-extensions")
    return l
}
```

#### 4.3.2 网络请求拦截（interceptor.go）

##### 核心机制：基于 CDP 网络事件

使用 Rod 的 `proto.NetworkResponseReceived` 事件被动监听网络响应，而非 `HijackRequests`（后者会修改请求流，可能触发反爬检测）。

##### NavigateAndIntercept 流程

```
NavigateAndIntercept(ctx, page, targetURL, rules) →
  1. 创建 results map[string]InterceptResult（key = rule.ID）
  2. 创建 done channel（当所有 rule 都匹配到时关闭）
  3. 启用网络事件监听：
     go page.EachEvent(func(e *proto.NetworkResponseReceived) {
         for _, rule := range rules {
             if strings.Contains(e.Response.URL, rule.URLPattern) {
                 // 通过 CDP 获取 response body
                 body := proto.NetworkGetResponseBody{RequestID: e.RequestID}
                 result, _ := body.Call(page)
                 results[rule.ID] = InterceptResult{
                     ID:   rule.ID,
                     Body: []byte(result.Body),
                     URL:  e.Response.URL,
                 }
                 if len(results) == len(rules) {
                     close(done)  // 所有规则都匹配到了
                 }
             }
         }
     })
  4. page.Navigate(targetURL)
  5. select {
         case <-done:    // 所有 API 响应已捕获
         case <-ctx.Done(): // 超时
     }
  6. 返回 results 切片
```

##### WaitForIntercept 流程

```
WaitForIntercept(ctx, page, rules) →
  1. 创建 resultCh channel
  2. 启动事件监听（同上，但匹配到后发送到 resultCh）
  3. 返回 resultCh 和 cancel 函数
  // 调用方在拿到 resultCh 后执行页面操作（点击翻页等），
  // 然后从 resultCh 读取结果
```

##### 超时处理

- 每次拦截操作都通过 `ctx` 控制超时
- 推荐超时：导航+拦截 30 秒（页面加载通常 2-5 秒，API 响应 < 1 秒）
- 超时返回 `context.DeadlineExceeded`，由上层 pool 的熔断机制处理

#### 4.3.3 登录态管理（auth.go）

##### EnsureLogin 决策流程

```
EnsureLogin(ctx, manager, loginURL, cookie, checker) →
  1. 从 Page 池借一个 Page
  2. 导航到 loginURL
  3. 调用 checker(ctx, page) 检查是否已登录
     ├── 已登录 → 归还 Page，返回 nil（user_data_dir 中有有效登录态）
     └── 未登录 →
         ├── headless=false → WaitForManualLogin(ctx, page, loginURL, checker)
         │   └── 用户手动登录后，登录态自动保存到 user_data_dir
         ├── headless=true && cookie != "" → InjectCookie(page, domain, cookie)
         │   └── 注入后再次 checker 验证
         └── headless=true && cookie == "" → log.Warn("匿名模式运行")
  4. 归还 Page
```

##### Bilibili 登录检测器（由 platform/bilibili 提供）

```go
// BilibiliLoginChecker checks if the browser is logged into Bilibili.
// Checks for the presence of "SESSDATA" cookie which indicates a valid login.
func BilibiliLoginChecker(ctx context.Context, page *rod.Page) (bool, error) {
    cookies, err := page.Cookies([]string{"https://www.bilibili.com"})
    if err != nil {
        return false, err
    }
    for _, c := range cookies {
        if c.Name == "SESSDATA" && c.Value != "" {
            return true, nil
        }
    }
    return false, nil
}
```

##### Cookie 注入

```go
func InjectCookie(page *rod.Page, domain string, cookieStr string) error {
    // 解析 "key1=value1; key2=value2" 格式
    // 对每个 key=value 调用:
    //   proto.NetworkSetCookie{Name, Value, Domain, Path: "/"}.Call(page)
}
```

#### 4.3.4 Bilibili 搜索爬虫（platform/bilibili/search.go）

##### SearchPage 核心流程

```
SearchPage(ctx, keyword, page) →
  1. mgr.GetPage() 借用一个浏览器 Page
  2. defer mgr.PutPage(page)
  3. 构造 URL: https://search.bilibili.com/video?keyword={keyword}&page={page}
  4. 定义拦截规则:
     rules := []InterceptRule{{
         URLPattern: "/x/web-interface/search/type",
         ID:         "search",
     }}
  5. results, err := NavigateAndIntercept(ctx, page, url, rules)
  6. 从 results["search"].Body 解析 JSON → SearchResp
  7. 转换 SearchResp.Data.Result → []Video
     - stripHTMLTags(title)  // 去除 <em> 标签
     - parseDuration(duration)
     - time.Unix(pubdate, 0)
  8. 构造 PageInfo{TotalPages, TotalCount}
  9. 返回 (videos, pageInfo, nil)
```

##### 与编排层的交互

```
RunStage0 调用链:
  pool.Run(tasks=pages, worker=func(page int) {
      searchCrawler.SearchPage(ctx, keyword, page)
      // 内部: GetPage → Navigate → Intercept → Parse → PutPage
  })
```

**注意**：编排层的 `requestInterval` 在浏览器方案下仍然生效（pool worker 之间的间隔），但实际意义不大——浏览器页面加载本身就需要 2-5 秒，天然形成了请求间隔。可以将 `requestInterval` 设为较小值（如 500ms）或 0。

#### 4.3.5 Bilibili 博主爬虫（platform/bilibili/author.go）

##### FetchAuthorInfo 核心流程

```
FetchAuthorInfo(ctx, mid) →
  1. mgr.GetPage() 借用一个浏览器 Page
  2. defer mgr.PutPage(page)
  3. 构造 URL: https://space.bilibili.com/{mid}
  4. 定义拦截规则（一次导航拦截两个 API）:
     rules := []InterceptRule{
         {URLPattern: "/x/space/acc/info", ID: "user_info"},
         {URLPattern: "/x/relation/stat",  ID: "user_stat"},
     }
  5. results, err := NavigateAndIntercept(ctx, page, url, rules)
  6. 解析 results["user_info"].Body → UserInfoResp
  7. 解析 results["user_stat"].Body → UserStatResp
  8. 返回 &AuthorInfo{
         Name:      infoResp.Data.Name,
         Followers: statResp.Data.Follower,
         Region:    "",
     }
```

##### FetchAuthorVideos 核心流程

```
FetchAuthorVideos(ctx, mid, page) →
  1. mgr.GetPage() 借用一个浏览器 Page
  2. defer mgr.PutPage(page)
  3. 构造 URL: https://space.bilibili.com/{mid}/video?pn={page}
     （B 站博主视频列表支持 URL 参数翻页）
  4. 定义拦截规则:
     rules := []InterceptRule{{
         URLPattern: "/x/space/wbi/arc/search",
         ID:         "video_list",
     }}
  5. results, err := NavigateAndIntercept(ctx, page, url, rules)
  6. 解析 results["video_list"].Body → VideoListResp
  7. 转换 VideoListResp.Data.List.Vlist → []VideoDetail
  8. 计算 PageInfo（totalPages = ceil(count / pageSize)）
  9. 返回 (videos, pageInfo, nil)
```

##### 与编排层的交互

```
RunStage1 调用链:
  pool.Run(tasks=mids, worker=func(mid AuthorMid) {
      // processOneAuthor 内部:
      authorCrawler.FetchAuthorInfo(ctx, mid.ID)
      // 内部: GetPage → Navigate → Intercept 2 APIs → Parse → PutPage
      
      authorCrawler.FetchAuthorVideos(ctx, mid.ID, 1)
      // 内部: GetPage → Navigate → Intercept → Parse → PutPage
      
      for p := 2; p <= actualPages; p++ {
          authorCrawler.FetchAuthorVideos(ctx, mid.ID, p)
      }
  })
```

**注意**：`processOneAuthor` 中每次调用 `FetchAuthorInfo` 和 `FetchAuthorVideos` 都会独立借用/归还 Page。这意味着同一个博主的多次请求可能使用不同的 Page，但这不影响正确性（每次都是独立的页面导航）。

**已知优化点**：当前设计中同一博主的 `FetchAuthorInfo` + 多次 `FetchAuthorVideos` 每次都独立借用/归还 Page，增加了不必要的 `about:blank` 导航开销。未来可考虑在 `processOneAuthor` 级别借用一个 Page 复用，减少 Page 切换次数。此优化不影响正确性，可在编码阶段根据实际性能表现决定是否实施。

#### 4.3.6 入口程序（cmd/crawler/main.go）

```
main() →
  1. 解析命令行参数:
     - keyword (必填)
     - platform (默认 "bilibili")
     - stage (默认 "all", 可选 "0", "1", "all")
     - config (默认 "conf/config.yaml")
  2. config.Load(configPath)
  3. cfg := config.Get()
  4. 创建浏览器 Manager:
     mgr, err := browser.New(browser.Config{
         Headless:    cfg.Browser.Headless,
         UserDataDir: cfg.Browser.UserDataDir,
         Concurrency: cfg.Concurrency,
     })
     defer mgr.Close()
  5. 登录态保障:
     browser.EnsureLogin(ctx, mgr, "https://www.bilibili.com",
         cfg.Cookie, bilibili.BilibiliLoginChecker)
  6. 创建平台实现:
     sc := bilibili.NewSearchCrawler(mgr)
     ac := bilibili.NewAuthorCrawler(mgr)
  7. 根据 stage 参数执行:
     if stage == "all" || stage == "0" {
         mids, err := src.RunStage0(ctx, sc, keyword, stage0Cfg)
     }
     if stage == "all" || stage == "1" {
         err := src.RunStage1(ctx, ac, mids, stage1Cfg)
     }
  8. 输出完成信息
```

#### 4.3.7 配置文件示例（conf/config.yaml.example）

```yaml
# 搜索与采集参数
max_search_page: 50        # 最大搜索页数 (1-50)
max_video_per_author: 1000 # 每个博主最多采集视频数
concurrency: 3             # 并发数（= 浏览器 Tab 数，推荐 3-5）

# 浏览器配置
browser:
  headless: true                      # 无头模式（服务器必须 true）
  user_data_dir: "data/browser-profile/" # 浏览器用户数据目录

# 熔断与间隔
max_consecutive_failures: 10  # 连续失败熔断阈值
request_interval: 500ms       # 请求间隔（浏览器方案下可设较小值）

# 输出
output_dir: "data/"

# 登录态（headless 模式下的降级方案）
cookie: ""  # 格式: "SESSDATA=xxx; bili_jct=xxx; ..."
```

### 4.4 方案优劣分析

#### 优势

| 维度 | 说明 |
|------|------|
| **反爬能力** | 浏览器行为与真实用户一致，绕过 IP 级累计阈值、wbi 签名等反爬机制 |
| **数据一致性** | 拦截的 JSON 与 API 直调完全相同，类型定义和解析逻辑可直接复用 |
| **架构兼容** | 编排层零修改，通过接口多态无缝切换，保留了所有通用模块 |
| **登录态持久化** | user-data-dir 自动保存 Cookie/Session，无需手动管理 Cookie 过期 |
| **跨平台通用** | 浏览器基础设施（Manager、拦截器、登录态）平台无关，未来接入 YouTube 等平台可复用 |
| **调试友好** | headless=false 时可以看到浏览器操作，方便排查问题 |

#### 劣势

| 维度 | 说明 | 缓解措施 |
|------|------|---------|
| **资源消耗** | 单个 Chromium 进程 ~200-400MB 内存，3 个 Tab 约 600MB-1.2GB | 限制 concurrency，服务器推荐 2-3 |
| **速度较慢** | 每次请求需要页面加载（2-5 秒），比 API 直调（~200ms）慢 10-20 倍 | 多 Tab 并发弥补；大规模任务下 API 方案反而更慢（触发风控后完全停滞） |
| **依赖 Chromium** | 需要下载 ~150MB 的 Chromium 二进制文件 | Rod 自动下载管理，首次运行一次性成本 |
| **页面变更风险** | B 站前端改版可能导致 URL 参数、API 路径变化 | API 路径相对稳定（后端接口）；前端 URL 变化只需修改 platform 层 |
| **无法分散风控** | 所有 Tab 共享同一 IP + Cookie，极端情况仍可能触发风控 | 非目标（代理 IP 池是独立优化方向） |
| **Chromium 下载依赖** | 首次运行时 Rod 需要自动下载 Chromium（~150MB），服务器网络受限时可能失败 | 支持手动预下载 Chromium 并通过环境变量指定路径 |
| **user_data_dir 磁盘增长** | 浏览器 profile 目录（缓存、历史记录等）可能随时间增长 | 定期清理或在启动时清除缓存子目录，仅保留 Cookie/Session |
| **页面 JS 渲染失败** | B 站 CDN 故障或 JS 加载失败时，目标 API 请求可能不触发 | 拦截超时机制兜底（30s），返回 error 由 pool 熔断处理 |

#### 与 API 方案的对比

| 维度 | API 直调（已废弃） | 浏览器自动化（本方案） |
|------|-------------------|---------------------|
| 小规模（~20 博主） | ✅ 快速、稳定 | ✅ 稳定，但较慢 |
| 大规模（600+ 博主） | ❌ 触发累计风控，完全失败 | ✅ 可完成（核心优势） |
| 资源消耗 | 极低（纯 HTTP） | 较高（Chromium 进程） |
| 实现复杂度 | 低（HTTP + JSON） | 中（浏览器管理 + 拦截） |
| 维护成本 | 高（wbi 签名、Cookie 预热等反爬对抗） | 低（浏览器自动处理签名和 Cookie） |

## 5. 备选方案 (Alternatives Considered)

### 5.1 API 直调方案（已实现后废弃）

- **描述**：直接调用 B 站 Web API（如 `wbi` 签名接口），纯 HTTP 请求获取数据
- **优点**：速度快、资源消耗极低
- **不选原因**：大规模场景（600+ 博主）下触发 B 站累计风控，导致任务完全失败；且需持续对抗 wbi 签名变更、Cookie 预热等反爬机制，维护成本高
- **代码去向**：已移至 `deprecated/api/` 目录
- **何时重新考虑**：如果 B 站开放官方 API 或放宽风控策略

### 5.2 代理 IP 池 + API 直调

- **描述**：通过购买代理 IP 池分散请求来源，配合 API 直调绕过风控
- **优点**：保留 API 直调的速度优势，理论上可规避 IP 维度的风控
- **不选原因**：需要持续付费购买代理 IP，增加金钱成本；当前场景对速度要求不高，浏览器方案已能满足需求
- **何时重新考虑**：如果未来对采集速度有极高要求，且预算允许

### 5.3 Selenium（Python 生态浏览器自动化）

- **描述**：使用 Selenium WebDriver 进行浏览器自动化
- **优点**：生态成熟，Python 社区广泛使用，文档丰富
- **不选原因**：本项目使用 Go 语言开发；Go 生态中 Rod 是更主流的选择，原生支持 CDP 协议，无需额外 WebDriver 中间层，更轻量高效
- **何时重新考虑**：如果项目技术栈切换到 Python

## 6. 业界调研 (Industry Research)

### 6.1 Go 生态浏览器自动化方案对比

| 方案 | 技术栈 | 特点 | 适用场景 |
|------|--------|------|----------|
| **Rod** | Go + CDP | API 简洁，原生 CDP，活跃社区，支持请求拦截/无头模式 | Go 项目首选 |
| **Chromedp** | Go + CDP | 更底层，API 偏原始，学习曲线陡 | 需要精细控制 CDP 的场景 |
| **Playwright** | Node/Python/Java + CDP | 微软出品，跨浏览器，功能最全面，无官方 Go SDK | 非 Go 项目 |
| **Puppeteer** | Node.js + CDP | Google 出品，仅 Chromium，Playwright 前身 | Node.js 项目 |
| **Scrapy + Splash** | Python | 传统爬虫 + 渲染引擎，架构较重 | Python 大规模爬虫 |

**选型结论**：Go 生态中 Rod 是最成熟的 CDP 库，API 设计与 Playwright 理念一致，且无需中间层。

### 6.2 请求拦截模式（业界最佳实践）

请求拦截是业界公认的**反反爬最佳实践**之一：让浏览器处理所有认证/签名/Cookie，程序只拦截并提取 API 响应数据。

- **Playwright**：`page.route()` 拦截 API 响应，或 `response` 事件被动监听
- **Puppeteer**：`page.setRequestInterception()` 主动拦截，或 `response` 事件被动监听
- **Rod**：`HijackRequests()` 主动拦截，或 `proto.NetworkResponseReceived` 事件被动监听

本方案采用 Rod 的 `proto.NetworkResponseReceived` 事件**被动监听**网络响应（见 §4.3.2），对页面行为零侵入，避免触发反爬检测。

### 6.3 登录态管理的业界实践

| 方案 | 描述 | 适用场景 |
|------|------|----------|
| **Cookie 持久化**（本方案采用） | 导出/导入浏览器 Cookie 文件 | 最常见，Cookie 有效期较长的平台 |
| **storageState 导入/导出** | Playwright 原生支持，包含 Cookie + localStorage | Playwright 项目 |
| **远程浏览器 + VNC** | 服务器上运行浏览器，通过 VNC/noVNC 远程手动登录 | Cookie 过期快的平台（如 YouTube） |

本方案采用 Cookie 持久化 + 远程调试端口方案，与业界主流实践一致。

## 7. 测试计划 (Test Plan)

### 7.1 单元测试

| 模块 | 测试内容 | 关键用例 |
|------|----------|----------|
| `browser/interceptor` | 请求拦截逻辑 | URL 子串匹配规则正确性；JSON 响应提取；多规则并发匹配；超时处理 |
| `platform/bilibili/search` | 搜索数据转换 | SearchResp JSON → `[]Video` + `PageInfo`；HTML 标签清理；时间格式转换；字段缺失处理 |
| `platform/bilibili/author` | 博主数据转换 | UserInfoResp/UserStatResp JSON → `AuthorInfo`；VideoListResp JSON → `[]VideoDetail`；字段缺失/异常值处理 |
| `browser/auth` | 登录态管理 | Cookie 字符串解析与注入；登录检测逻辑；EnsureLogin 策略分支（headless/GUI/匿名） |

### 7.2 集成测试

| 场景 | 测试内容 | 验证点 |
|------|----------|--------|
| 端到端采集 | 启动真实浏览器 → 访问 B 站博主页 → 拦截 API 响应 → 解析数据 | 能成功拦截并解析出视频列表 |
| Cookie 持久化 | 保存 Cookie → 重启浏览器 → 加载 Cookie | 加载后无需重新登录 |
| 多页翻页 | 采集某博主全部视频（多页） | 翻页逻辑正确，数据不重复不遗漏 |
| 错误恢复 | 模拟网络超时/页面加载失败 | 重试机制生效，不丢失已采集数据 |

### 7.3 手动测试

| 场景 | 操作 | 验收标准 |
|------|------|----------|
| 首次登录 | 通过远程调试端口打开浏览器 → 手动扫码登录 → 保存 Cookie | Cookie 文件生成成功 |
| 全量采集 | 配置 600+ 博主列表 → 启动采集 | 全部博主数据采集完成，无遗漏 |
| 反爬应对 | 长时间运行观察 | 无被封禁/验证码中断 |

## 8. 可观测性 & 运维 (Observability & Operations)

### 8.1 日志

| 级别 | 内容 |
|------|------|
| **INFO** | 采集开始/结束、博主切换、翻页进度、Cookie 加载成功 |
| **WARN** | 请求超时重试、Cookie 即将过期、响应数据异常 |
| **ERROR** | 浏览器启动失败、Cookie 过期需重新登录、数据解析失败、采集中断 |

日志格式：复用现有项目的 `log.Printf` 标准库格式（`LEVEL: message` 模式），与 `crawler.go`、`pool.go` 等模块保持一致。浏览器模块的日志增加 `[browser]` 前缀标识，如 `log.Printf("INFO: [browser] Page pool created, size=%d", concurrency)`。

### 8.2 关键指标

| 指标 | 说明 |
|------|------|
| 采集进度 | 已完成博主数 / 总博主数 |
| 单博主耗时 | 每个博主从开始到完成的时间 |
| 拦截成功率 | 成功拦截的 API 请求数 / 总翻页请求数 |
| 错误次数 | 按错误类型分类统计 |

### 8.3 运维操作

| 操作 | 方式 |
|------|------|
| Cookie 过期处理 | 日志 ERROR 提示 → 手动通过远程调试端口重新登录 → 保存新 Cookie |
| 断点续采 | 基于已采集博主列表，跳过已完成的博主继续采集 |
| 采集状态查看 | 日志输出当前进度，支持查看已完成/待采集/失败的博主列表 |

## 9. Changelog
| 日期 | 变更内容 | 作者 |
|------|----------|------|
| 2026-03-27 | 初始创建 | AI |
| 2026-03-27 | 完成需求澄清，填写 §1-3（背景、目标、需求） | AI + User |
| 2026-03-27 | 需求变更：废弃 API 方案，浏览器方案作为唯一采集方式；API 代码移至 `deprecated/api/` | User |
| 2026-03-27 | 完成 §4.1 方案概览 | AI + User |
| 2026-03-27 | 完成 §4.2 组件设计（模块设计、接口、数据模型、并发模型、错误处理） | AI + User |
| 2026-03-27 | 完成 §4.3 核心逻辑实现 + §4.4 方案优劣分析 | AI |
| 2026-03-27 | 完成 §5 备选方案 + §6 业界调研 + §7 测试计划 + §8 可观测性 & 运维 | AI |
| 2026-03-27 | AI 评审修复：统一 §6.2 拦截方式描述与 §4.3.2 一致；修正 §7.1 单元测试模块名；§4.4 补充 Chromium 下载/磁盘增长/JS 渲染失败风险；§8.1 日志格式改为复用现有 log.Printf；§4.3.5 标注 Page 复用优化点 | AI |

## 10. 参考资料 (References)
