---
name: bp-coding-best-practices
description: 通用编码最佳实践。在编写或 review 代码时使用。涵盖可读性、命名、函数设计、控制流、资源安全、注释规范。
---

# 通用编码最佳实践

**设计原则（SOLID、设计模式）**：参见 `bp-component-design` Skill
**特定语言/模块规范**：参见相应的 standards skills

---

## 命名

| 原则 | 说明 |
|------|------|
| **自解释** | `retryCount` 而非 `n` |
| **无魔法数字** | `const int SECONDS_IN_DAY = 86400;` |
| **布尔命名** | `isValid`, `hasAccess`（问题形式） |
| **作用域匹配** | 小作用域可短（`i`），大作用域要描述性 |

---

## 函数设计

| 原则 | 说明 |
|------|------|
| **单一职责** | 一个函数做一件事；名字需要 "And" 说明做太多了 |
| **参数精简** | 超过 3-4 个参数 → 考虑结构体封装 |
| **const 正确** | 不修改的参数标 `const`，防止意外修改 |

---

## 控制流

**Guard Clause**：失败情况先处理并返回，主逻辑保持左对齐

**Early Return**：显式采用 early return 编程范式，尽量将可 early return 的检查前置。

```go
// ❤ 深层嵌套
if order != nil {
    if order.IsValid() {
        if order.HasItems() {
            // main logic
        }
    }
}

// ✅ Guard Clause
if order == nil {
    return
}
if !order.IsValid() {
    return
}
if !order.HasItems() {
    return
}
// main logic (not nested)
```
---

## 资源安全

| 原则 | 说明 |
|------|------|
| **defer 清理** | 使用 defer 确保资源释放（文件、连接、锁等） |
| **所有权显式** | 区分谁负责关闭资源 |
| **窄作用域** | 变量声明靠近首次使用，减少误用概率 |

跨语言场景统一要求：新增分支/返回路径时，必须检查资源契约是否闭环。

---

## 注释

| 场景 | 做法 |
|------|------|
| **何时写** | 仅当意图不明显时；复杂算法；公共 API |
| **写什么** | **Why**（为什么这样做），不是 What（做了什么） |
| **TODO** | 包含上下文和负责人 |

```go
// ❤ 复述代码
// Increment i by 1
i++

// ✅ 解释意图
// Skip index 0 because it is the sentinel slot.
for i := 1; i < len(slots); i++ { ... }
```
---

## 可观测性（日志）

| 场景 | 做法 |
|------|------|
| **关键分支覆盖** | 至少覆盖无数据快速返回、异常状态转换、错误返回三个分支 |
| **级别选择** | `DEBUG` 记录成功路径和排障上下文，`WARN` 记录异常但可恢复路径，`ERROR` 记录失败路径 |
| **上下文信息** | 日志中携带最小必要上下文（如 row/cf/ret/request_id），避免无上下文日志 |

---

## 进阶

- **可读性详细规则**：[reference/readability.md](reference/readability.md)
- **安全性详细规则**：[reference/safety.md](reference/safety.md)
