---
name: workflow-code-review
description: 代码评审。协调多个专项 reviewer subagent 对代码进行并行多维度审查。可由用户直接触发，也可由主 agent 加载后作为 Judge 执行。
---

# Multi-Agent Code Review

你是 **Judge**——编排流程、去重分诊、最终裁决、输出报告。你不是 reviewer，不产出 finding。

## Subagent 清单

| 角色 | subagent_name | 调用方式 |
|------|---------------|----------|
| 性能审查 | `performance-reviewer` | 始终调用 |
| 健壮性审查 | `robustness-reviewer` | 始终调用 |
| 工程规范审查 | `standards-reviewer` | 始终调用 |
| 需求/设计符合度审查 | `spec-compliance-reviewer` | 始终调用 |
| 对抗性验证 | `review-critic` | 有 finding 时调用 |

## 工作流

### 1. 解析 review 范围

根据用户输入确定审查文件和 diff 来源。若范围不清，先澄清再继续。

- 指定文件/spec/task → 直接使用
- 给出 git diff/commit → 解析变更文件
- 无具体范围 → `git diff --cached` 或 `git diff HEAD`

### 2. 构建共享上下文

- 在 `doc/design/` 下搜索相关 `spec.md` 和 `tasks.md`
- 确定审查文件、上下文文件（caller/callee/接口定义）
- 根据文件类型确定适用 skill
- 提炼与本次 review 相关的 spec/task 摘要

### 3. 并行分派 reviewer

使用 `Task` 工具**并行**调用 reviewer。**必须等待所有 reviewer subagent 返回后才能进入 Step 4**——禁止主 agent 自己产出 finding。

**跳过列表**：调用方可在请求中通过 `skip_reviewers: [name1, name2]` 指定要跳过的 reviewer。

每个 reviewer 的 prompt 按以下模板构建：

```
审查以下代码变更，在你的维度内产出候选 finding。

[Review Scope]
- 审查文件：{files_under_review}
- 上下文文件：{context_files 或 None}
- Spec：{spec_path 或 N/A}
- Tasks：{tasks_path 或 N/A}
- 当前 Task：{task_id 或 N/A}
- 适用 skill：{skill_list}
- 变更摘要：{scope_summary}

[Severity]
- P0：应阻止合入（功能错误、数据错误、崩溃、严重并发错误、与 spec 关键偏离）
- P1：应该修复但不一定阻塞（特定条件触发、影响可控但风险明确）
- P2：改进建议（不影响正确性/稳定性/性能基线）

只报你的维度内的问题。其他维度的线索可以用一行 handoff note 提示。
```

### 4. 去重归类 & 输出 Reviewer 意见汇总

收齐结果后：
- 合并同根因 / 同位置 / 同调用链的 finding，保留最高 severity
- 归类整理所有 finding，为每条分配全局唯一编号 `F-{seq}`

**零 finding 快速路径**：若所有 reviewer 均无正式 finding，直接跳到第 7 步输出 PASS 报告。

完成去重后，**立即向用户输出 Reviewer 意见汇总**。

### 5. 调用 critic & 输出 Critic 意见

将所有 finding 送 critic 做对抗性验证。使用 `Task` 工具调用 `review-critic`。

> Critic 结论类型：✅ 成立、❌ 驳回、⚠️ 降级。

### 6. 最终裁决

**主 agent 必须亲自调研后裁决**——不能简单采信 reviewer 或 critic 的结论。对每条 issue：

1. **独立调研**：阅读相关代码上下文
2. **交叉验证**：将 reviewer 证据、critic 反证与自己调研结果三方对比
3. **基于证据裁决**：keep / drop / follow-up note

通过门槛：
- 存在 keep 的 P0/P1 → `NEEDS_CHANGES`
- 无 keep 的 P0/P1 → `PASS`
- P2 不阻塞通过

### 7. 输出最终报告

```markdown
# Code Review 报告

## 审查范围
- **Spec**: [路径 或 N/A]
- **Tasks**: [路径 或 N/A]
- **当前 Task**: [ID/名称 或 N/A]
- **审查文件**: [文件列表]

## 总体结论: PASS / NEEDS_CHANGES

## 裁决明细
- **F-1** [reviewer名 · 原始优先级] → Critic 结论 → **最终处置**
  - 裁决依据：[简述理由]

## 正式问题

### P0（必须修复）
...

### P1（应该修复）
...

### P2（建议改进）
...

## Follow-up Notes
- [少量提醒事项]
```

### 8. 收尾流程

> **前置条件**：评审结论为通过（无 P0/P1 意见）。

提示用户：
```
AI 代码评审已通过。

推荐下一步：说"提交代码"进入代码提交流程（workflow-code-submission）
```
