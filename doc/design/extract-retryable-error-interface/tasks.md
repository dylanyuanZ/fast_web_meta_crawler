# 实施任务清单

> 将 `isRetryableError` 从 crawler 编排层的硬编码实现抽取为接口，具体实现放到 platform 下
> 任务总数: 3
> 核心原则: 先建后迁后删——先在 platform 下创建实现，再注入到编排层，最后删除硬编码

## 依赖关系总览

```
Task 1 (在 bilibili/errors.go 创建 IsRetryableError 实现)
  ↓
Task 2 (crawler.go: Stage2Config 新增字段 + retryWithCooldown 改用注入函数 + 删除 isRetryableError)  ← 依赖 Task 1
  ↓
Task 3 (main.go: 注入 bilibili.IsRetryableError)  ← 依赖 Task 2
```

## 变更影响概览

### 文件变更清单

| 文件 | 操作 | 涉及任务 | 说明 |
|------|------|---------|------|
| `src/platform/bilibili/errors.go` | 新建 | Task 1 | Bilibili 平台特定的可重试错误判断 |
| `src/crawler.go` | 修改 | Task 2 | Stage2Config 新增 IsRetryableError 字段；retryWithCooldown 改用注入函数；删除 isRetryableError |
| `cmd/crawler/main.go` | 修改 | Task 3 | 组装 Stage2Config 时注入 bilibili.IsRetryableError |

## 任务列表

### 任务 1: [x] 在 bilibili 包下创建 IsRetryableError 实现
- 文件: `src/platform/bilibili/errors.go`（新建）
- 依赖: 无
- spec 映射: 无 spec（小型重构）
- 说明: 将 `crawler.go` 中硬编码的 Bilibili 特定错误判断逻辑（`status=412`、`intercept timeout`）迁移到 `bilibili/errors.go`，导出为 `IsRetryableError(error) bool` 函数。
- context:
  - `src/crawler.go:isRetryableError()` — 原始实现（迁移源）
  - `src/platform/bilibili/` — 目标包
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `bilibili.IsRetryableError` 函数已导出
  - [ ] 包含 `status=412` 和 `intercept timeout` 两种错误判断
- 子任务:
  - [ ] 1.1: 创建 `src/platform/bilibili/errors.go`，实现 `IsRetryableError(error) bool`

### 任务 2: [x] crawler.go 编排层改用注入的 IsRetryableError
- 文件: `src/crawler.go`（修改）
- 依赖: Task 1
- spec 映射: 无 spec（小型重构）
- 说明:
  1. `Stage2Config` 新增 `IsRetryableError func(error) bool` 字段
  2. `retryWithCooldown` 新增 `isRetryable func(error) bool` 参数，替代直接调用 `isRetryableError`
  3. `RunStage2` 中调用 `retryWithCooldown` 时传入 `cfg.IsRetryableError`
  4. 删除 `isRetryableError` 函数
- context:
  - `src/crawler.go:retryWithCooldown()` — 直接修改目标
  - `src/crawler.go:RunStage2()` — 调用方
  - `src/crawler.go:Stage2Config` — 配置结构体
  - `cmd/crawler/main.go` — 上游组装方（Task 3 处理）
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `crawler.go` 中不再包含 `status=412` 或 `intercept timeout` 字符串
  - [ ] `Stage2Config` 包含 `IsRetryableError` 字段
  - [ ] `retryWithCooldown` 通过参数接收错误判断函数
- 子任务:
  - [ ] 2.1: `Stage2Config` 新增 `IsRetryableError func(error) bool` 字段
  - [ ] 2.2: `retryWithCooldown` 签名新增 `isRetryable func(error) bool` 参数
  - [ ] 2.3: `RunStage2` 调用 `retryWithCooldown` 时传入 `cfg.IsRetryableError`
  - [ ] 2.4: 删除 `isRetryableError` 函数及其注释

### 任务 3: [x] main.go 注入 bilibili.IsRetryableError
- 文件: `cmd/crawler/main.go`（修改）
- 依赖: Task 2
- spec 映射: 无 spec（小型重构）
- 说明: 在 `main.go` 组装 `Stage2Config` 时，注入 `bilibili.IsRetryableError`。
- context:
  - `cmd/crawler/main.go` — Stage2Config 组装处
  - `src/platform/bilibili/errors.go` — 实现方
- 验收标准:
  - [ ] `go build ./...` 编译通过
  - [ ] `Stage2Config` 的 `IsRetryableError` 字段被正确赋值为 `bilibili.IsRetryableError`
- 子任务:
  - [ ] 3.1: 在 Stage2Config 初始化中添加 `IsRetryableError: bilibili.IsRetryableError`
