---
description: 强制在处理任何任务前检查并使用相关技能
alwaysApply: true
---

# Skill 使用规则

**核心规则**：收到用户消息后，先判断是否有 skill 适用（哪怕 1% 可能）→ 有就调用 `use_skill`，按 skill 指示执行。

## 路由

- Workflow 顺序：requirements-clarification → system-design → code-generation → test-generation → code-review → code-submission
- **路由决策**：收到用户需求后，按以下优先级判断入口 skill：
  1. **需求是否清晰？** → 如果需求模糊（术语不明确、目标不清晰、涉及范围不确定），无论是否涉及代码修改，都**必须先调用 `workflow-requirements-clarification`**
  2. **需求清晰 + 涉及代码修改** → 调用 `workflow-code-generation`，它会评估复杂度并在 spec 缺失时引导回 `workflow-requirements-clarification`
  3. **需求清晰 + 不涉及代码修改** → 直接回答或调用对应 workflow
- **判断"需求清晰"的标准**：AI 能明确回答以下三个问题：(1) 要改什么模块/文件？(2) 要实现什么行为？(3) 怎样算完成？—— 三个都能回答才算清晰

## ⚠️ 需求澄清前禁止代码搜索

**强制规则**：当判定需求不清晰、需要走 `workflow-requirements-clarification` 时，**直接进入需求澄清流程**，禁止以"先了解上下文"为由提前搜索代码。

- 代码调研是需求澄清流程内部 Step 4 的职责，不是前置动作
- 违反此规则的典型表现：AI 先 `codebase_search` / `grep_search` / `read_file`，然后才说"需要走需求澄清流程"

| ✗ 错误 | ✓ 正确 |
|--------|--------|
| 需求不清 → 先搜代码 → 再说"要走需求澄清" | 需求不清 → 直接进入需求澄清流程 |
| "让我先了解一下上下文再决定" | "需求不够清晰，进入需求澄清流程" |

## Workflow 衔接检查

**强制规则**：每个 Workflow 完成时，必须执行该 Workflow SKILL.md 中的**所有步骤**，**执行完全部步骤后才能切换到下一个 Workflow**。

### Workflow 间衔接约束

| 前置 Workflow | 后继 Workflow | 衔接条件 |
|--------------|--------------|---------|
| RC | SD | RC 全部步骤执行完毕，spec.md 前三章节完整 |
| SD | CG | SD 全部步骤执行完毕（含 AI 评审），spec.md 设计章节完整 |
| CG | TG | CG 全部步骤执行完毕，所有编码任务完成 |
| TG | CR | TG 全部步骤执行完毕，引导进入 CR |
| CR | CS | CR 通过后，引导进入 CS |

### 跨 Workflow 反模式

| ❌ 错误 | ✅ 正确 |
|---------|--------|
| 跳过 SKILL.md 收尾步骤直接提示"进入下一阶段" | 执行完 SKILL.md 全部步骤，再切换 Workflow |
