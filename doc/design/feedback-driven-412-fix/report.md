# 反馈驱动开发记录：修复 Crawler 卡住问题

## 1. 背景

### 1.1 问题描述

运行以下命令时，程序会**永久卡住**，无法正常结束：

```bash
go run cmd/crawler/main.go -keyword "炸鸡" -stage all -config "conf/config.yaml" -platform "bilibili" -limit 20
```

表现为：
- Stage 0（搜索页面爬取）在处理到第 17 页左右时卡住
- Stage 1（博主详情爬取）在处理到第 13 个博主左右时卡住
- 进程永远不退出，chromium 子进程持续占用资源

### 1.2 验证目标

命令成功结束，且 `authors.csv` 中有 **20 条完整数据**。

### 1.3 方法论

采用**反馈驱动开发方法**（Feedback-Driven Development），遵循"修改 → 编译 → 运行 → 分析 → 修改"的快速迭代循环，由小及大逐步验证。

---

## 2. 问题分析

通过代码审查，发现了 **4 个关键 Bug**，它们共同导致了程序卡住：

### Bug 1: `WaitForIntercept` 遇到 412 时永久阻塞 ⚡ 最严重

**文件**: `src/browser/interceptor.go`

**根因**：当 B 站 API 返回 412（风控响应），`WaitForIntercept` 中的事件监听器会跳过这个非 2xx 响应（`continue`），继续等待一个**永远不会来的** 2xx 响应。而调用方 `FetchAllAuthorVideos` 传入的 `ctx` 来自 `context.Background()`，**没有 deadline**，导致永久阻塞。

**问题代码**：
```go
if e.Response.Status < 200 || e.Response.Status >= 300 {
    log.Printf("WARN: skipping non-2xx response...")
    continue  // ← 跳过 412，继续等待永远不会来的 2xx
}
```

### Bug 2: `NavigateAndIntercept` 遇到 412 时等 30s 超时

**文件**: `src/browser/interceptor.go`

**根因**：与 Bug 1 相同的逻辑问题。虽然 `NavigateAndIntercept` 有 30s 默认超时不会永久卡住，但每次遇到 412 都要白等 30s 才能失败，严重拖慢整体进度。

### Bug 3: Rod 操作不受 context 超时控制 ⚡ 最严重

**文件**: `src/browser/extractor.go` + `src/browser/interceptor.go`

**根因**：Rod 库的 `page.Navigate()`、`page.WaitLoad()`、`page.Eval()` 使用的是 page 内部的 context（`page.ctx`），而不是我们通过 `context.WithTimeout` 创建的 ctx。代码中虽然创建了超时 context，但**从未传递给 Rod**，导致所有超时机制完全无效。

**调用链分析**：
```
pool worker ctx (context.Background(), 无 deadline)
  → processOneAuthor(ctx)
    → FetchAllAuthorVideos(ctx)
      → WaitForIntercept(ctx, page, rules)
        → page.EachEvent(...)  // 使用 page.ctx，不是我们的 ctx
```

当页面加载缓慢或卡住时，`page.Navigate()` 永远不返回，page 无法归还到池中，最终所有 page 都被耗尽，整个程序卡死。

**验证方式**：查阅 Rod v0.116.2 源码 `context.go`，确认 `page.Context(ctx)` 是正确的 API——它返回一个使用指定 ctx 的 page 克隆。

### Bug 4: 412 风控无重试机制，并发 worker 同时被 412

**文件**: `src/crawler.go` + `src/pool/pool.go`

**根因**：修复 Bug 1-3 后，程序不再卡住，但 B 站在并发请求密集时会触发 412 风控。由于没有重试机制，遇到 412 的博主直接失败。更严重的是，当多个 worker 同时遇到 412 后同时重试，又会再次触发 412，形成"重试风暴"。

---

## 3. 迭代过程

### 迭代 1: 修复 interceptor 的 412 永久阻塞（Bug 1 + Bug 2）

**优化思路**：当 `WaitForIntercept` 和 `NavigateAndIntercept` 遇到非 2xx 响应时，不应该默默跳过继续等待，而应该**立即返回错误**，让上层快速失败并继续处理下一个博主。

**修改内容**：
- `NavigateAndIntercept`：新增 `non2xxErrCh` channel，事件监听器遇到非 2xx 时立即发送错误并停止监听（`return true`），外层 select 优先检查该 channel
- `WaitForIntercept`：同样新增 `non2xxErrCh` channel + 添加默认 30s 超时（`defaultInterceptTimeout`），防止无 deadline 的 ctx 导致永久阻塞

**修改文件**：`src/browser/interceptor.go`

**验证结果**：编译通过 ✅

### 迭代 2: 修复 Rod context 超时无效（Bug 3）

**优化思路**：Rod 的 page 操作使用 `page.ctx` 而非函数参数中的 ctx。必须通过 `page.Context(ctx)` 创建带超时的 page 克隆，才能让 `Navigate`/`WaitLoad`/`Eval` 受超时控制。

**修改内容**：
- `NavigateAndExtract`：在所有 page 操作前调用 `p := page.Context(ctx)`，后续操作使用 `p` 而非 `page`
- `NavigateAndIntercept`：同样在 Navigate 前调用 `p := page.Context(ctx)`

**修改文件**：`src/browser/extractor.go` + `src/browser/interceptor.go`

**验证结果**：编译通过 ✅，limit 3 测试通过（56s 完成，3/3 成功）✅

### 迭代 3: limit 20 首次测试

**运行命令**：
```bash
go run cmd/crawler/main.go -keyword "炸鸡" -stage all -config "conf/config.yaml" -platform "bilibili" -limit 20
```

**验证结果**：程序不再卡住 ✅，2 分 17 秒完成。但 **success=15, failed=5**，5 个博主因 412 风控失败 ❌

**分析**：
- 程序不再永久阻塞，遇到 412 能快速失败 → Bug 1-3 修复有效
- 并发 3 个 worker 同时请求，B 站在处理到第 15 个博主左右时触发 412 风控
- 没有重试机制，遇到 412 直接放弃

### 迭代 4: 添加 412 重试机制

**优化思路**：在 `processOneAuthor` 中加入重试逻辑——当遇到包含 "status=412" 的错误时，使用指数退避（10s → 20s → 40s）等待后重试，最多重试 3 次。

**修改内容**：
- 将原 `processOneAuthor` 重命名为 `processOneAuthorOnce`
- 新 `processOneAuthor` 包装重试逻辑：检测 412 错误 → 指数退避等待 → 重试
- 新增 `is412Error` 辅助函数，使用 `strings.Contains(err.Error(), "status=412")` 判断

**修改文件**：`src/crawler.go`

**验证结果**：程序完成，**success=16, failed=4** → 比上次多恢复了 1 个博主，但仍有 4 个失败 ❌

**分析**：
- 重试机制生效，部分博主重试后成功
- 但 4 个 worker 同时遇到 412 后**同时重试**，导致重试请求也被 412
- 需要一个**全局冷却机制**：当任何 worker 遇到 412 时，所有 worker 都暂停

### 迭代 5: 添加全局冷却机制（Global Cooldown）

**优化思路**：当某个 worker 遇到 412 时，不仅当前 worker 要等待，还需要**通知所有 worker 暂停**。这样可以避免"重试风暴"——多个 worker 同时重试又同时被 412。

**设计方案**：

```
Worker A 遇到 412
  → 触发 Cooldown.Trigger(10s)  // 设置全局冷却 10s
  → 所有 Worker 在下一个任务前调用 Cooldown.Wait()
  → 全部暂停 10s
  → 冷却结束后恢复正常
```

**修改内容**：

1. **`src/pool/pool.go`** — 新增 `Cooldown` 结构体：
   - `Trigger(d)`: 设置冷却期，如果已有更长的冷却期则忽略
   - `Wait(ctx)`: 阻塞直到冷却期结束或 ctx 取消
   - `pool.Run` 新增可选的 `cooldown ...*Cooldown` 参数（variadic，向后兼容）
   - Worker 在每个任务前调用 `cd.Wait(ctx)`

2. **`src/crawler.go`** — 集成全局冷却：
   - `Stage1Config` 新增 `Cooldown *pool.Cooldown` 字段
   - `RunStage1` 中创建 `Cooldown` 实例并传给 worker
   - `processOneAuthor` 重试时调用 `cfg.Cooldown.Trigger(backoff)` 触发全局冷却
   - Worker 函数在每个任务前调用 `cd.Wait(ctx)` 等待冷却

**修改文件**：`src/pool/pool.go` + `src/crawler.go`

**验证结果**：
```
Authors: success=20, failed=0
Task completed in 2m22s
```
**20/20 成功，0 失败** ✅🎉

**日志验证**：
```
WARN: [pool] Global cooldown triggered: all workers pausing for 10s
WARN: Retrying author 芸合影视 (mid=3546572535519498) in 10s (attempt 1/3)
INFO: [browser] WaitForIntercept matched video_list ...  // 重试成功
```

---

## 4. 修复总结

| # | Bug | 严重度 | 文件 | 修复策略 |
|---|-----|--------|------|----------|
| 1 | `WaitForIntercept` 遇到 412 永久阻塞 | ⚡ Critical | `src/browser/interceptor.go` | 非 2xx 立即返回错误 + 默认 30s 超时 |
| 2 | `NavigateAndIntercept` 遇到 412 等 30s | 🔶 High | `src/browser/interceptor.go` | 非 2xx 立即返回错误 |
| 3 | Rod 操作不受 context 超时控制 | ⚡ Critical | `src/browser/extractor.go` + `interceptor.go` | `page.Context(ctx)` 传递超时 |
| 4 | 412 无重试 + 并发重试风暴 | 🔶 High | `src/crawler.go` + `src/pool/pool.go` | 指数退避重试 + 全局冷却机制 |

### 关键优化思路

1. **快速失败（Fail Fast）**：遇到不可恢复的错误（如 412）时立即返回，而不是默默等待超时。这将单个 412 的处理时间从 30s 降低到 < 1s。

2. **超时传递（Context Propagation）**：第三方库（Rod）的 API 不一定使用 Go 标准的 `context.Context` 参数。必须查阅文档确认正确的超时传递方式（Rod 使用 `page.Context(ctx)` 而非函数参数）。

3. **指数退避重试（Exponential Backoff）**：对可重试错误（412 风控）使用 10s → 20s → 40s 的退避策略，给 B 站的风控窗口足够的冷却时间。

4. **全局冷却（Global Cooldown）**：当任何 worker 触发风控时，所有 worker 同步暂停。这避免了"重试风暴"——多个 worker 同时重试又同时被风控。这是从"单 worker 重试"到"协调式重试"的关键升级。

---

## 5. 迭代效果对比

| 迭代 | 修复内容 | limit | 成功 | 失败 | 耗时 | 状态 |
|------|---------|-------|------|------|------|------|
| 修复前 | — | 20 | ~13 | — | ∞（卡住） | ❌ 永久阻塞 |
| 迭代 1-2 | Bug 1-3 | 3 | 3 | 0 | 56s | ✅ 小规模通过 |
| 迭代 3 | Bug 1-3 | 20 | 15 | 5 | 2m17s | ⚠️ 412 无重试 |
| 迭代 4 | + 重试 | 20 | 16 | 4 | ~3m | ⚠️ 重试风暴 |
| 迭代 5 | + 全局冷却 | 20 | **20** | **0** | **2m22s** | ✅ 完美通过 |

---

## 6. 经验教训

### 6.1 第三方库 API 验证

> **教训**：不要假设第三方库的 API 行为与 Go 标准库一致。Rod 的 `page.Navigate()` 不接受 `context.Context` 参数，而是通过 `page.Context(ctx)` 设置。使用前必须查阅文档或源码确认。

### 6.2 防御性超时

> **教训**：任何可能阻塞的操作都必须有超时保护。即使上层已经设置了 context timeout，也要在关键阻塞点添加默认超时作为兜底，防止 context 未正确传递时导致永久阻塞。

### 6.3 协调式重试

> **教训**：在并发爬虫场景中，单 worker 重试不够——当多个 worker 同时被风控时，它们会同时重试，再次触发风控。需要全局协调机制（如 Cooldown）让所有 worker 同步暂停。

### 6.4 由小及大验证

> **教训**：反馈驱动开发的核心原则——先用小规模（limit 3）验证基本功能，再扩大到目标规模（limit 20）。这样可以快速定位问题层次：limit 3 通过说明基本逻辑正确，limit 20 失败说明是并发/风控问题。
