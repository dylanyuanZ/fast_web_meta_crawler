---
name: workflow-code-generation
description: 代码文件修改的统一入口。当用户请求任何代码变更（新功能、优化、Bug 修复、重构）时必须首先调用此 skill。仅适用于代码文件（如 .go/.py/.js 等），修改 .md 等非代码文件时不需要调用。它会评估复杂度、检查 spec.md、生成 tasks.md、并逐个任务执行。
---

# 代码生成

> 所有代码修改的统一入口。**先加载规范，再写代码。**

## 工作流程

### 步骤 1：评估复杂度与路径路由

> ⚠️ **防御性检查**：如果 AI 无法明确回答"要改什么模块/文件"、"要实现什么行为"、"怎样算完成"这三个问题中的任何一个，则**立即停止**，调用 `workflow-requirements-clarification`。本 skill 不负责需求澄清。

**默认走标准流程**（步骤 2 → 3 → 4）。Fast-Path 仅当以下条件**全部满足**时才可进入：

| # | 条件 | 说明 |
|---|------|------|
| 1 | 单文件、局部改动 | 能在路由阶段确定完整文件列表，且只涉及 1 个文件的局部修改 |
| 2 | 无 spec.md 关联 | 不是从 workflow 衔接进来的 |

> **任一条件不满足或无法确定 → 标准流程。**

#### Fast-Path 执行

1. 加载编码规范（同步骤 3）
2. 实现改动
3. 执行过程中发现实际需要改多个文件 → **立即退出**，回到步骤 2 进入标准流程
4. 询问用户是否需要 code review
   - **需要** → 使用 `use_skill` 调用 `workflow-code-review`
   - **不需要** → 输出改动说明
5. **结束**，不进入后续步骤

#### 标准流程入口

查找 `doc/design/<feature>/spec.md`：
- 存在且完整 → **仔细通读全文**，确认理解
- 不存在/不完整 → **调用 `workflow-requirements-clarification`**（禁止自行澄清）

**强制**：`spec.md` 与 `tasks.md` 同时存在时，编码前必须完整读取两者。

### 步骤 2：检查/创建 tasks.md

- **已存在** → 找第一个未完成任务，进入步骤 3
- **不存在** → **先读取** [reference/task_planning_guide.md](reference/task_planning_guide.md)，然后严格按其流程创建 `tasks.md`

> 🚨 **创建 tasks.md 后必须停下来等用户确认。** 展示任务列表，然后**停止并等待用户回复**。禁止自动进入步骤 3。

### 步骤 3：加载编码规范（🚨 强制前置）

> **未加载规范就写代码 → 立即停止，先加载。**

#### 必须加载

| 规范 | 说明 |
|------|------|
| `bp-coding-best-practices` | 通用编码最佳实践 |
| `bp-performance-optimization` | 性能优化 |

#### 按需加载

| 规范 | 何时加载 |
|------|----------|
| `std-company-go` | `.go` 文件 |

### 步骤 4：逐个任务实现

**核心规则：一个 Task → review 通过 → 报告 → 等用户批准 → 下一个 Task**

#### Phase 1: 实现

修改代码，更新 tasks.md 标记 In Progress。

#### Phase 1.5: 编译验证（🚨 强制，不可跳过）

代码修改后、进入 Code Review 前，**必须**验证编译：

```bash
cd /data/workspace_vscode/fast_web_meta_crawler && go build ./...
```

**编译不通过 → 修复后重新编译，直到通过。**

#### Phase 2: Code Review 与修复循环（🚨 强制）

**2a. 执行 Code Review**

使用 `use_skill` 工具调用 `workflow-code-review` skill。

**2b. 逐条反思犯错原因**

对报告中**每条** keep 的 finding，反思犯错原因：

| 原因分类 | 含义 |
|---------|------|
| **Spec 理解偏差** | spec 写清楚了但理解错误 |
| **规范未遵守** | 编码规范有要求但未执行 |
| **执行遗漏** | 漏掉边界/细节 |
| **设计考虑不足** | 需更深层设计思考 |

**2c. 自动修复 → Re-review 循环**

修复 finding → 重新调用 `workflow-code-review` 执行 re-review → 重复，直到结论为 PASS。

**2d. 输出 Review 总结**

所有轮次结束（PASS）后，向用户输出一份总结。

#### Phase 3: 汇报 → 停止等待

输出报告，更新 tasks.md 标记 Completed，**停止等待用户批准**。

#### Task 完成报告模板

```markdown
---
## ✅ Task N 完成

### 改动文件
- `path/to/file.go`: [改动说明]

### 改动内容
[做了什么，为什么]

### 🤖 Code Review 结果

**Review 轮次**: [第 K 轮通过]
[附 workflow-code-review 输出的报告]

---

选项：
- **继续/LGTM** → 下一个任务
- **修改** → 重新提交（用户附带修改要求）
- **回滚** → 撤销本次改动
```

---

## 用户跳过 spec 时

必须生成简化版 spec.md（标注 `Status: Quick Draft`）。**禁止无 spec 修改中等+代码。**

## 恢复中断的任务

读取 `tasks.md` → 找未完成任务 → **重新加载编码规范** → 继续执行。
