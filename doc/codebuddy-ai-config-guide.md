# CodeBuddy AI 配置体系说明

> 本文档说明 `.codebuddy/` 目录下各配置文件的作用，帮助团队成员理解 AI 辅助开发的配置体系。

## 1. 概览

`.codebuddy/` 目录构成了一套 **AI 辅助开发的配置体系**，本质上是在告诉 AI "你是谁、怎么做事、做事的标准是什么"。

四大模块的关系如下：

```
Rules（性格 & 底线）    ──── 始终生效 ────→  AI 助手
Skills（专业技能）      ──── 按需加载 ────→  AI 助手
Commands（快捷指令）    ──── 用户触发 ────→  Skills
Agents（专家分身）      ──── 被 Skills 调度 → AI 助手
```

## 2. Rules — 始终生效的"行为准则"

> 📁 `.codebuddy/rules/`

Rules 是 **永远生效** 的全局规则，AI 每次回答都会遵守。相当于给 AI 设定了"性格"和"底线"。

| 文件 | 作用 |
|------|------|
| `thinking_principles.md` | AI 的身份（全栈工程师）和行为约束（先读代码再回答、不做假设、证据导向等） |
| `output_style.md` | 输出风格（跟随用户语言、代码用英文、文档用中文） |
| `enforcing_skill_usage.md` | 强制 AI 在处理任务前先检查是否有 skill 可用，并定义 workflow 路由逻辑 |
| `memory-usage-policy.md` | 知识沉淀策略（项目知识必须写入文件，不能只存 memory） |

**核心价值**：确保 AI 的行为一致性和可预测性，无论谁在使用，AI 都遵循相同的准则。

## 3. Skills — 按需加载的"专业技能包"

> 📁 `.codebuddy/skills/`

Skills 是 **按需动态加载** 的指令集，只有在需要时才会被激活。每个 skill 有一个 `SKILL.md`（主指令）和可选的 `reference/` 目录（参考资料）。

### 3.1 Workflow 类（流程型）

定义了完整的研发流程，按顺序衔接：

| Skill | 作用 | 触发时机 |
|-------|------|---------|
| `workflow-requirements-clarification` | 需求澄清，生成 spec.md 前三章节（背景、目标、需求） | 需求模糊时自动触发 |
| `workflow-system-design` | 系统设计，填充 spec.md 设计章节 | 需求明确后触发 |
| `workflow-code-generation` | 代码生成，拆分任务并逐个执行 | 设计完成后触发 |
| `workflow-test-generation` | 测试生成 | 编码完成后触发 |
| `workflow-code-review` | 代码评审（多维度并行审查） | 测试完成后触发 |
| `workflow-code-submission` | 代码提交（分支命名、commit 规范） | 评审通过后触发 |

### 3.2 Best Practice 类（知识型）

编码规范和设计原则，在编码和评审过程中被引用：

| Skill | 作用 |
|-------|------|
| `bp-coding-best-practices` | 通用编码最佳实践（可读性、命名、函数设计等） |
| `bp-architecture-design` | 架构设计原则（模块划分、依赖管理等） |
| `bp-component-design` | 组件设计原则（类/模块设计、接口设计等） |
| `bp-performance-optimization` | 性能优化指南 |
| `std-company-go` | Go 编码规范（基于 Google Golang 代码规范） |
| `self-refinement` | 自我纠错，将经验沉淀为规则 |
| `bp-skill-authoring` | Skill 编写指南 |

## 4. Commands — 用户触发的"快捷指令"

> 📁 `.codebuddy/commands/`

Commands 是 **快捷方式**，本质上是对 skills 的简写调用。用户可以通过快捷指令快速触发对应的 workflow。

| 文件 | 作用 |
|------|------|
| `code-generation.md` | 触发 `workflow-code-generation` skill |
| `code-review.md` | 触发 `workflow-code-review` skill |
| `gen-commit-msg.md` | 生成规范的 commit message |
| `reflect.md` | 触发自我反思（`self-refinement` skill） |
| `troubleshooting.md` | 问题排查 |
| `requirements-clarification.md` | 触发需求澄清 |
| `system-design.md` | 触发系统设计 |
| `skill-authoring.md` | 触发 skill 编写 |
| `test-generation.md` | 触发测试生成 |
| `performance-optimization.md` | 触发性能优化 |
| `debug-build-clang.md` | Debug 构建 |
| `release-build-clang.md` | Release 构建 |

## 5. Agents — 被调度的"专家分身"

> 📁 `.codebuddy/agents/`

Agents 是 **子角色**，由 skills 在内部调度。每个 agent 有特定的专业视角，主要在代码评审和任务规划中发挥作用。

| Agent | 角色 | 使用场景 |
|-------|------|---------|
| `codebase-researcher.md` | 代码库调研员 | 分析现有代码结构和依赖 |
| `task-planner.md` | 任务规划师 | 将需求拆分为可执行的任务 |
| `test-writer.md` | 测试编写者 | 生成单元测试和集成测试 |
| `performance-reviewer.md` | 性能审查员 | 审查代码性能问题 |
| `robustness-reviewer.md` | 健壮性审查员 | 审查错误处理和边界情况 |
| `spec-compliance-reviewer.md` | 规范合规审查员 | 检查代码是否符合 spec 要求 |
| `standards-reviewer.md` | 编码标准审查员 | 检查代码是否符合编码规范 |
| `review-critic.md` | 评审质量把关者 | 对审查结果做二次质量把关 |

**典型调度场景**：`workflow-code-review` skill 执行时，会并行调度 `performance-reviewer`、`robustness-reviewer`、`standards-reviewer`、`spec-compliance-reviewer` 四个 agent 从不同维度审查代码，最后由 `review-critic` 对审查结果做质量把关。

## 6. 实际效果对比：以"帮我实现一个计算器的功能"为例

### 6.1 没有配置文件时

```
用户：帮我实现一个计算器的功能

AI：好的！这是一个简单的计算器实现：
    （直接甩出一大段代码）
    func Calculate(a, b float64, op string) float64 {
        switch op {
        case "+": return a + b
        case "-": return a - b
        ...
    }
    完成了！
```

**问题**：
- 没有澄清需求（命令行？Web？支持哪些运算？精度要求？）
- 没有设计阶段，直接写代码
- 代码风格随意，没有遵循项目规范
- 没有测试
- 没有 commit 规范

### 6.2 有配置文件时

整个过程会按 workflow 流转：

**Step 1 — Rules 生效，路由判断**

```
thinking_principles: 需求不清晰，不能直接写代码
enforcing_skill_usage: 路由到 workflow-requirements-clarification
```

AI 会先澄清需求：
- 计算器是命令行工具还是 Web 接口？
- 需要支持哪些运算？（四则运算？科学计算？）
- 是否需要处理表达式解析（如 "2+3*4"）？
- 精度要求？整数还是浮点？

**Step 2 — workflow-requirements-clarification**

生成 `spec.md` 前三章节（背景、目标、需求），确保需求理解一致。

**Step 3 — workflow-system-design**

- `task-planner` agent 参与规划
- `bp-architecture-design` 指导模块划分
- `bp-component-design` 指导组件设计
- 填充 `spec.md` 设计章节（如：词法分析器、语法解析器、求值器）

**Step 4 — workflow-code-generation**

- 拆分为 `tasks.md`（Task 1: Tokenizer, Task 2: Parser...）
- 逐个任务编码
- `std-company-go` 确保 Go 代码符合规范
- `bp-coding-best-practices` 确保代码质量

**Step 5 — workflow-test-generation**

- `test-writer` agent 生成单元测试
- 覆盖边界情况（除零、空输入、嵌套括号等）

**Step 6 — workflow-code-review**

- `performance-reviewer` 审查性能
- `robustness-reviewer` 审查健壮性
- `standards-reviewer` 审查编码规范
- `spec-compliance-reviewer` 审查是否符合 spec
- `review-critic` 对审查结果做质量把关

**Step 7 — workflow-code-submission**

- 规范的 commit message
- 提交前检查

### 6.3 对比总结

| 维度 | 没有配置 | 有配置 |
|------|---------|--------|
| **需求理解** | 直接猜测，可能做错方向 | 先澄清，确保理解一致 |
| **设计** | 无设计，边写边想 | 有 spec.md 沉淀，架构清晰 |
| **代码质量** | 随意风格 | 遵循 Go 规范 + 最佳实践 |
| **测试** | 通常没有 | 自动生成，覆盖边界情况 |
| **评审** | 无 | 多维度并行审查 |
| **知识沉淀** | 下次从零开始 | spec.md + rules 持续积累 |
| **一致性** | 每次回答风格不同 | 始终遵循相同的行为准则 |

## 7. 总结

> 没有这些配置时，AI 是一个"随叫随到但没有章法的实习生"；
> 有了这些配置后，AI 变成了一个"遵循完整研发流程的高级工程师"。

这套配置体系的核心价值在于：
1. **流程标准化**：从需求到提交，每个环节都有明确的规范
2. **质量可控**：多维度审查确保代码质量
3. **知识沉淀**：经验和规范以文件形式跟随项目仓库，不依赖个人记忆
4. **团队一致性**：无论谁使用 AI，都遵循相同的准则和流程
