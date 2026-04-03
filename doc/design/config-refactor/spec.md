# 配置结构重构

## 1. 背景与问题

### 1.1 现状

当前配置文件 `conf/config.yaml` 存在以下结构性问题：

**问题 1：`max_search_page` 和 `max_video_per_author` 对 YouTube 无效**

- `max_search_page`：仅在 Bilibili 的 `SearchAndRecord` 中使用（`actualPages = min(TotalPages, cfg.MaxSearchPage)`），YouTube 完全未读取此值。
- `max_video_per_author`：仅在 `Stage2Config` 中传递给 `FetchAllAuthorVideos`，而 YouTube 的 Stage 2 是 no-op（`FetchAllAuthorVideos` 直接返回 nil）。
- 这两个参数理应是**平台无关**的通用限制，但实际只对 Bilibili 生效。

**问题 2：`max_search_page` 语义不合理**

- YouTube 是无限滚动加载，没有"页"的概念。YouTube 自己用 `max_scroll_count` 控制滚动次数，但这与 `max_search_page` 功能重叠且不互通。
- 用户真正关心的是"最多搜索多少条视频"，而非"翻多少页"或"滚动多少次"。

**问题 3：并发配置冗余且不合理**

- 当前有 `stage0/1/2` 三层并发配置（每层含 `concurrency`、`request_interval`、`max_consecutive_failures`），加上全局 fallback，共产生 12 个 getter 方法。
- 不同平台在同一 stage 下的最佳并发数完全不同（如 YouTube Stage 0 只能单线程滚动，Bilibili Stage 0 可以多页并发）。
- 按 stage 分层的配置粒度不对——应该按**平台**分层，因为风控策略是平台决定的。

### 1.2 当前配置结构

```yaml
# 全局搜索配置
max_search_page: 1           # 仅 Bilibili 使用
max_video_per_author: 1      # 仅 Bilibili Stage 2 使用

# 全局并发配置
concurrency: 1
request_interval: 2500ms
max_consecutive_failures: 10

# 按 stage 分层配置（覆盖全局值）
# stage0/stage1/stage2: concurrency, request_interval, max_consecutive_failures

# 平台配置
platform:
  bilibili:
    cookie: "..."
  youtube:
    filter_type: "video"
    filter_upload: "week"
    # max_scroll_count: 0     # YouTube 专属，与 max_search_page 功能重叠
```

### 1.3 涉及的代码模块

| 模块 | 文件 | 职责 |
|------|------|------|
| config | `src/config/config.go` | 配置结构体定义、加载、默认值、clamp、getter 方法 |
| config | `conf/config.yaml` | 用户配置文件 |
| orchestration | `cmd/crawler/main.go` | 配置传递到各 Stage |
| orchestration | `src/crawler.go` | Stage0/1/2 编排逻辑、StageConfig 结构体 |
| bilibili | `src/platform/bilibili/search.go` | 使用 `cfg.MaxSearchPage`、`cfg.Concurrency` |
| youtube | `src/platform/youtube/search.go` | 使用 `ytCfg.MaxScrollCount`、`cfg.RequestInterval` |

## 2. 目标

1. **统一搜索限制**：用 `max_search_videos` 替代 `max_search_page`，对所有平台生效，语义为"最多搜索到的视频数量"。
2. **统一作者视频限制**：让 `max_video_per_author` 对所有平台生效（YouTube 当前 Stage 2 是 no-op，但配置应预留）。
3. **简化并发配置**：删除 `stage0/1/2` 分层配置，将并发参数（`concurrency`、`request_interval`）下沉到 `platform` 级别，全局值作为 fallback。
4. **删除冗余配置**：删除 YouTube 的 `max_scroll_count`（被 `max_search_videos` 替代）。
5. **保持向后兼容**：配置文件变更后，`bin/crawler` 命令行用法不变，两个平台均能正常工作。

### 完成标准

- [ ] `max_search_page` 被 `max_search_videos` 替代，Bilibili 内部自动换算为页数
- [ ] `max_search_videos` 对 YouTube 生效（滚动循环中检查 `totalVideos >= max_search_videos` 时停止）
- [ ] `max_video_per_author` 对所有平台生效（即使 YouTube 当前 Stage 2 是 no-op）
- [ ] `stage0/1/2` 分层配置及其 12 个 getter 方法被删除
- [ ] YouTube 的 `max_scroll_count` 被删除
- [ ] 并发参数（`concurrency`、`request_interval`）可在 `platform.<name>` 下配置，全局值作为 fallback
- [ ] `conf/config.yaml` 更新为新结构
- [ ] 编译通过，`bin/crawler --platform youtube --keyword "中创星航" --config="conf/config.yaml"` 正常运行
- [ ] `bin/crawler --platform bilibili --keyword "中创星航" --config="conf/config.yaml"` 正常运行

## 3. 需求

### 3.1 配置文件新结构

```yaml
# 全局搜索配置
max_search_videos: 100       # 最大搜索视频数量（替代 max_search_page）
max_video_per_author: 1000   # 每个作者最大视频数

# 全局并发配置（作为平台未配置时的 fallback）
concurrency: 3               # 默认并发数
request_interval: 2500ms     # 默认请求间隔
max_consecutive_failures: 10 # 连续失败次数上限

# 浏览器配置
browser:
  headless: true
  user_data_dir: "data/browser-profile/"
  bin: "/usr/bin/google-chrome"

# 输出配置
output_dir: "data/"

# 平台配置
platform:
  bilibili:
    cookie: "..."
    concurrency: 3            # Bilibili 可以稍高并发
    request_interval: 500ms
  youtube:
    filter_type: "video"
    filter_upload: "week"
    concurrency: 1            # YouTube 风控严格，建议低并发
    request_interval: 2500ms
```

### 3.2 功能需求

#### FR-1：`max_search_page` → `max_search_videos`

- 删除 `max_search_page` 配置项和相关常量/clamp 逻辑
- 新增 `max_search_videos` 配置项（默认值 100，范围 1-10000）
- **Bilibili 适配**：在 `SearchAndRecord` 中将 `max_search_videos` 换算为页数（`ceil(max_search_videos / pageSize)`），与 API 返回的 `TotalPages` 取较小值
- **YouTube 适配**：在滚动循环中增加 `totalVideos >= cfg.MaxSearchVideos` 的退出条件

#### FR-2：删除 YouTube `max_scroll_count`

- 从 `YouTubeConfig` 结构体中删除 `MaxScrollCount` 字段
- 从 YouTube `SearchAndRecord` 中删除 `maxScroll` 相关逻辑
- `max_search_videos` 完全替代其功能

#### FR-3：并发配置下沉到平台级别

- 从 `Config` 结构体中删除 `Stage0`、`Stage1`、`Stage2` 字段及其 12 个 getter 方法
- 在 `BilibiliConfig` 和 `YouTubeConfig` 中新增 `Concurrency`、`RequestInterval` 字段
- 新增平台级 getter 方法（如 `GetPlatformConcurrency(platform string) int`），优先返回平台配置，fallback 到全局值
- `main.go` 中构建 `Stage1Config`/`Stage2Config` 时使用新的平台级 getter

#### FR-4：`max_video_per_author` 对所有平台生效

- 保持 `max_video_per_author` 作为全局配置
- 确保 Bilibili 的 `FetchAllAuthorVideos` 和 YouTube 的 `FetchAllAuthorVideos`（即使是 no-op）都能接收到此参数
- 无需在平台级别重复配置

### 3.3 非功能需求

- **NFR-1**：不引入新的外部依赖
- **NFR-2**：配置加载失败时给出清晰的错误信息
- **NFR-3**：删除的配置项不保留 deprecated 标记（无历史包袱）

## 4. 方案概览

### 4.1 整体思路

重构配置层（`config.go` + `config.yaml`），将搜索限制统一为视频数量语义，将并发配置从 stage 分层改为 platform 分层，全局值作为 fallback。各消费方继续通过 `config.Get()` 读取配置，不改变依赖方向。

### 4.2 涉及的模块和边界

| 模块 | 改动类型 | 说明 |
|------|---------|------|
| `config` | **核心改动** | 结构体重构、删除 stage 分层、新增平台级并发字段、getter 方法重写 |
| `config.yaml` | **配置文件** | 更新为新结构 |
| `main.go` | **适配** | 页面池大小改为取平台并发数；删除 `Stage0/1/2Config` 构建逻辑 |
| `src/crawler.go` | **适配** | `StageConfig` 简化（删除 stage 分层相关字段），各 Stage 从 `config.Get()` 读取平台级配置 |
| `bilibili/search.go` | **适配** | `max_search_page` → `max_search_videos` 换算逻辑 |
| `youtube/search.go` | **适配** | 删除 `max_scroll_count`，改用 `max_search_videos` 作为退出条件 |
| `browser/browser.go` | **不改** | `browser.Config` 结构不变，只是传入的 `Concurrency` 值来源变了 |

### 4.3 依赖方向（不变）

```
main.go → config.Get() → Config 结构体
main.go → browser.New(browser.Config{...})
platform/bilibili/* → config.Get()
platform/youtube/* → config.Get()
src/crawler.go → config.Get()
```

所有模块单向依赖 `config` 包，`config` 不依赖任何业务模块。本次重构不改变依赖方向。

### 4.4 数据流

```
config.yaml
  → config.Load() → Config 结构体（全局单例）
  → config.Get() 被各模块直接调用

main.go:
  concurrency := config.Get().GetPlatformConcurrency("youtube")
  browser.New(browser.Config{Concurrency: concurrency, ...})

youtube/search.go:
  cfg := config.Get()
  maxVideos := cfg.MaxSearchVideos  // 直接读全局配置
  interval := cfg.GetPlatformRequestInterval("youtube")  // 平台级，fallback 到全局
```

### 4.5 关键 trade-off

| 决策 | 收益 | 代价 |
|------|------|------|
| 并发配置按平台而非按 stage | 符合实际（风控是平台决定的） | 同一平台不同 stage 无法配置不同并发数（目前不需要此能力） |
| 全局 config 单例 + `Get()` | 简单，不需要到处传参 | 测试时需要 mock 全局状态 |
| `max_search_videos` 替代 `max_search_page` | 语义统一，平台无关 | Bilibili 需要内部换算为页数 |
| 删除 `max_scroll_count` | 减少重复配置 | YouTube 失去精确控制滚动次数的能力（但该能力本身不合理） |

## 5. 组件设计

### 5.1 配置结构体重构

#### 删除的结构体和字段

- 删除 `StageConfig` 结构体（整个删除）
- 删除 `Config` 中的 `Stage0`、`Stage1`、`Stage2` 字段
- 删除 `Config` 中的 `MaxSearchPage` 字段
- 删除 `YouTubeConfig` 中的 `MaxScrollCount` 字段

#### 新增/修改的结构体

```go
// BilibiliConfig — 新增并发字段
type BilibiliConfig struct {
    Cookie          string        `yaml:"cookie"`
    Concurrency     int           `yaml:"concurrency"`      // 新增
    RequestInterval time.Duration `yaml:"request_interval"` // 新增
}

// YouTubeConfig — 删除 MaxScrollCount，新增并发字段
type YouTubeConfig struct {
    FilterType      string        `yaml:"filter_type"`
    FilterDuration  string        `yaml:"filter_duration"`
    FilterUpload    string        `yaml:"filter_upload"`
    SortBy          string        `yaml:"sort_by"`
    Concurrency     int           `yaml:"concurrency"`      // 新增
    RequestInterval time.Duration `yaml:"request_interval"` // 新增
}

// Config — 删除 Stage0/1/2 和 MaxSearchPage，新增 MaxSearchVideos
type Config struct {
    OutputDir              string         `yaml:"output_dir"`
    Browser                BrowserConfig  `yaml:"browser"`
    Platform               PlatformConfig `yaml:"platform"`
    MaxSearchVideos        int            `yaml:"max_search_videos"`    // 替代 max_search_page
    MaxVideoPerAuthor      int            `yaml:"max_video_per_author"`
    Concurrency            int            `yaml:"concurrency"`          // 全局 fallback
    MaxConsecutiveFailures int            `yaml:"max_consecutive_failures"`
    RequestInterval        time.Duration  `yaml:"request_interval"`     // 全局 fallback
    Cookie                 string         `yaml:"cookie"`
}
```

### 5.2 Getter 方法变更

#### 删除 12 个 Stage getter

删除 `GetStage0Concurrency`、`GetStage0RequestInterval`、`GetStage0MaxConsecutiveFailures`、`GetStage1Concurrency`、`GetStage1RequestInterval`、`GetStage1MaxConsecutiveFailures`、`GetStage2Concurrency`、`GetStage2RequestInterval`、`GetStage2MaxConsecutiveFailures`。

#### 新增 2 个平台级 getter

```go
// GetPlatformConcurrency 返回指定平台的有效并发数。
// 优先级：平台配置 > 全局 fallback。
func (c *Config) GetPlatformConcurrency(platform string) int

// GetPlatformRequestInterval 返回指定平台的有效请求间隔。
// 优先级：平台配置 > 全局 fallback。
func (c *Config) GetPlatformRequestInterval(platform string) time.Duration
```

### 5.3 默认值和校验变更

- 删除 `DefaultMaxSearchPage` 及相关常量
- 新增 `DefaultMaxSearchVideos = 100`，范围 `[1, 10000]`
- 新增平台级 `Concurrency` 的 clamp（范围 `[1, 16]`，仅在值 > 0 时 clamp）
- `MaxConsecutiveFailures` 不下沉到平台级，全局统一

### 5.4 接口设计

本次重构**不新增接口**，不改变 `SearchRecorder` / `AuthorCrawler` 接口签名。变更仅在接口实现内部。

**对外 API 变更汇总**：

| 变更 | 旧 API | 新 API |
|------|--------|--------|
| 搜索限制 | `cfg.MaxSearchPage` | `cfg.MaxSearchVideos` |
| 并发数 | `cfg.GetStage1Concurrency()` | `cfg.GetPlatformConcurrency("bilibili")` |
| 请求间隔 | `cfg.GetStage1RequestInterval()` | `cfg.GetPlatformRequestInterval("bilibili")` |
| 失败阈值 | `cfg.GetStage1MaxConsecutiveFailures()` | `cfg.MaxConsecutiveFailures`（直接读全局） |
| YouTube 滚动限制 | `ytCfg.MaxScrollCount` | 删除，改用 `cfg.MaxSearchVideos` |

### 5.5 数据模型（config.yaml 新结构）

```yaml
# 全局搜索配置
max_search_videos: 100       # 最大搜索视频数量（替代 max_search_page）
max_video_per_author: 1000   # 每个作者最大视频数

# 全局并发配置（平台未配置时的 fallback）
concurrency: 3
request_interval: 2500ms
max_consecutive_failures: 10

# 浏览器配置
browser:
  headless: true
  user_data_dir: "data/browser-profile/"
  bin: "/usr/bin/google-chrome"

# 输出配置
output_dir: "data/"

# 平台配置
platform:
  bilibili:
    cookie: "..."
    concurrency: 3
    request_interval: 500ms
  youtube:
    filter_type: "video"
    filter_upload: "week"
    concurrency: 1
    request_interval: 2500ms
```

Schema 迁移：无需迁移工具。旧配置文件中的 `max_search_page`、`stage0/1/2` 字段会被 YAML 解析器忽略（Go 的 `yaml.Unmarshal` 默认忽略未知字段），不会报错。

### 5.6 并发模型

并发模型本身不变。变更点仅在于**并发数的来源**：

| 场景 | 旧来源 | 新来源 |
|------|--------|--------|
| browser.New 页面池大小 | `cfg.Concurrency`（全局） | `cfg.GetPlatformConcurrency(platform)`（平台级） |
| Stage 1 Worker Pool | `cfg.GetStage1Concurrency()` | `cfg.GetPlatformConcurrency(platform)` |
| Stage 2 Worker Pool | `cfg.GetStage2Concurrency()` | `cfg.GetPlatformConcurrency(platform)` |
| Bilibili Stage 0 搜索翻页 | `cfg.Concurrency`（直接读） | `cfg.GetPlatformConcurrency("bilibili")` |
| YouTube Stage 0 滚动 | 单线程（不受并发配置影响） | 不变，仍然单线程 |

### 5.7 错误处理

本次重构不引入新的错误模式。配置校验：

- `MaxSearchVideos` 范围 `[1, 10000]`，超出范围 clamp 并 log WARN
- 平台级 `Concurrency` 范围 `[1, 16]`，超出范围 clamp 并 log WARN
- 与现有 `clampInt` 机制一致，无需新增错误处理逻辑

### 5.8 调用方适配汇总

#### `main.go` 变更

- `browser.New` 页面池大小改为 `cfg.GetPlatformConcurrency(*platform)`
- `Stage1Config` / `Stage2Config` 构建时使用 `cfg.GetPlatformConcurrency(*platform)` 和 `cfg.GetPlatformRequestInterval(*platform)`
- `MaxConsecutiveFailures` 直接读 `cfg.MaxConsecutiveFailures`

#### `bilibili/search.go` 变更

- `max_search_page` → `max_search_videos` 换算：`maxPages = ceil(cfg.MaxSearchVideos / pageSize)`
- Worker Pool 并发数改为 `cfg.GetPlatformConcurrency("bilibili")`

#### `youtube/search.go` 变更

- 删除 `maxScroll` 相关逻辑
- 新增 `totalVideos >= cfg.MaxSearchVideos` 退出条件

## 6. 任务分解

> 待 code-generation 阶段填写
