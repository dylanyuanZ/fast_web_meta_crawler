# 实施任务清单

> Bug 修复：拦截器 status code 检查 + 翻页卡住 + 翻页效率优化
> 任务总数: 3
> 核心原则: 先修防御性检查（拦截器），再修调用方逻辑（翻页），最后优化效率

## 依赖关系总览

```
Task 1 (拦截器增加 HTTP status code 检查)
  ↓
Task 2 (FetchAuthorVideos 翻页卡住修复)  ← 依赖 Task 1
  ↓
Task 3 (翻页效率优化：接口重构 + 单页面连续翻页)  ← 依赖 Task 2
```

## 变更影响概览

### 文件变更清单

| 文件 | 操作 | 涉及任务 | 说明 |
|------|------|---------|------|
| `src/browser/interceptor.go` | 修改 | Task 1 | 拦截器增加 status code 过滤 |
| `src/platform/bilibili/author.go` | 修改 | Task 2, 3 | FetchAuthorVideos 翻页逻辑重构 |
| `src/crawler.go` | 修改 | Task 3 | 修改 AuthorCrawler 接口 |
| `src/platform/bilibili/probe_test.go` | 修改 | Task 3 | 更新测试代码 |

## 任务列表

### 任务 1: [x] 拦截器增加 HTTP status code 检查

- 文件: `src/browser/interceptor.go`（修改）
- 依赖: 无
- spec 映射: 调试修复，无 spec 章节
- 说明:
  当前 `NavigateAndIntercept` 和 `WaitForIntercept` 中的 `EachEvent` 回调只检查 URL 匹配（第 84 行 `strings.Contains(e.Response.URL, rule.URLPattern)`），不检查 HTTP status code。当 B 站返回 412（风控）时，响应 body 是 HTML 错误页面，但仍被当作有效 JSON 数据捕获，导致后续 JSON 解析失败。

  **修复方案**: 在 URL 匹配后增加 status code 检查，只捕获 2xx 响应。对于非 2xx 响应，打印 WARN 日志并跳过（继续等待下一个匹配的响应，或最终超时）。

- context:
  - `src/browser/interceptor.go` — 直接修改目标
  - `src/platform/bilibili/author.go:FetchAuthorVideos()` — 上游调用方
  - `src/platform/bilibili/author.go:FetchAuthorInfo()` — 上游调用方
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `NavigateAndIntercept` 和 `WaitForIntercept` 均增加了 status code 检查
  - [ ] 非 2xx 响应被跳过并打印 WARN 日志
- 子任务:
  - [ ] 1.1: 在 `NavigateAndIntercept` 的 `EachEvent` 回调中，URL 匹配后增加 `e.Response.Status < 200 || e.Response.Status >= 300` 检查，不匹配则 WARN 并 continue
  - [ ] 1.2: 在 `WaitForIntercept` 的 `EachEvent` 回调中做同样的修改

### 任务 2: [x] FetchAuthorVideos 翻页卡住修复

- 文件: `src/platform/bilibili/author.go`（修改）
- 依赖: Task 1
- spec 映射: 调试修复，无 spec 章节
- 说明:
  当前 `FetchAuthorVideos(page=N)` 对 N>1 的处理是：先 `NavigateAndIntercept` 拦截 page 1 数据，然后点击翻页按钮 N-1 次。当 Step 1 拦截到的 page 1 数据是无效的（如 412 响应被 Task 1 过滤后超时，或 JSON 解析失败），Step 2 仍然会尝试点击翻页按钮并 `WaitForIntercept`，导致卡住 30 秒。

  **修复方案**: 在 Step 1 完成后立即验证 `videoBody` 的有效性（尝试 JSON 解析检查 `code` 字段）。如果 page 1 数据无效，直接返回错误，不进入 Step 2 翻页循环。

- context:
  - `src/platform/bilibili/author.go:FetchAuthorVideos()` — 直接修改目标
  - `src/browser/interceptor.go:NavigateAndIntercept()` — 被调用方
  - `src/crawler.go:processOneAuthor()` — 上游调用方，翻页循环在此处
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] Step 1 拦截失败或数据无效时，直接返回错误，不进入 Step 2
  - [ ] `processOneAuthor` 的翻页循环能正确处理此错误（已有 break 逻辑）
- 子任务:
  - [ ] 2.1: 在 `FetchAuthorVideos` 的 Step 1 之后、Step 2 之前，对 `videoBody` 做快速有效性检查（JSON 解析检查 `code` 字段是否为 0）
  - [ ] 2.2: 如果检查失败，返回包含具体原因的错误信息

### 任务 3: [x] 翻页效率优化：接口重构 + 单页面连续翻页

- 文件: `src/crawler.go`（修改）、`src/platform/bilibili/author.go`（修改）、`src/platform/bilibili/probe_test.go`（修改）
- 依赖: Task 2
- spec 映射: 调试修复，无 spec 章节
- 说明:
  当前 `processOneAuthor` 对每一页都调用 `FetchAuthorVideos(page=N)`，而每次调用都会重新 `GetPage` + 导航到 video 页面 + 从 page 1 开始点击翻页。获取 N 页数据需要 N 次导航 + N*(N-1)/2 次点击，效率极低且增加被风控的风险。

  **修复方案**: 直接修改 `AuthorCrawler` 接口，用 `FetchAllAuthorVideos(ctx, mid, maxVideos)` 替代 `FetchAuthorVideos(ctx, mid, page)`。新方法内部一次导航、从头翻到尾，翻一页拿一页数据。同时简化 `processOneAuthor`，移除翻页循环。

- context:
  - `src/crawler.go:AuthorCrawler` — 接口定义，直接修改
  - `src/crawler.go:processOneAuthor()` — 调用方，简化翻页循环
  - `src/platform/bilibili/author.go:FetchAuthorVideos()` — 删除，替换为 FetchAllAuthorVideos
  - `src/platform/bilibili/probe_test.go` — 更新测试适配新接口
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `AuthorCrawler` 接口中 `FetchAuthorVideos` 替换为 `FetchAllAuthorVideos`
  - [ ] `FetchAllAuthorVideos` 内部：1 次导航 + N-1 次点击翻页
  - [ ] `processOneAuthor` 不再有翻页循环，一次调用获取所有视频
  - [ ] probe_test.go 测试代码适配新接口
- 子任务:
  - [ ] 3.1: 修改 `src/crawler.go` 中 `AuthorCrawler` 接口：`FetchAuthorVideos(ctx, mid, page)` → `FetchAllAuthorVideos(ctx, mid, maxVideos)`
  - [ ] 3.2: 简化 `processOneAuthor`：移除翻页循环，直接调用 `FetchAllAuthorVideos`
  - [ ] 3.3: 在 `author.go` 中删除旧 `FetchAuthorVideos`，实现 `FetchAllAuthorVideos`（导航 → 拦截 page 1 → 解析总页码 → 循环点击翻页拦截后续页 → 返回所有视频）
  - [ ] 3.4: 更新 `probe_test.go` 中的测试代码适配新接口

---

## Spec 覆盖映射

| 来源 | 任务 | 说明 |
|------|------|------|
| 调试分析：问题 1 | Task 1 | 拦截器不检查 HTTP status code |
| 调试分析：问题 2 | Task 2 | FetchAuthorVideos 翻页卡住 |
| 调试分析：问题 3 | Task 3 | 翻页效率极低 |
