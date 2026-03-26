---
name: workflow-code-submission
description: 代码提交的统一入口。管理分支命名、commit message 规范、提交前检查。可独立触发或由其他 workflow（如 code-review）调用。
---

# 代码提交

## 核心定位

管理从**分支创建**到**代码推送**的完整流程，包含所有 Git 操作规范和提交前检查。

## 触发条件

| 场景 | 触发方式 |
|------|----------|
| 用户说"提交代码"、"commit" | 独立触发 |
| `workflow-code-review` CR 通过后 | 由 code-review 调用 |

---

## Step 0: 获取用户信息

```bash
git config user.name
```

- **成功** → 用作 `{user}`（转小写，空格替换为 `-`）
- **失败** → 默认值 `dev`

---

## Step 1: 分支管理

### 1.1 分支命名规范

```
{type}/{short-desc}
```

| 字段 | 说明 |
|------|------|
| `{type}` | `feat` / `fix` / `refactor` / `opt` |
| `{short-desc}` | 2-4 个英文单词，用 `-` 连接 |

**示例**：
- `feat/bilibili-search-api`
- `fix/csv-export-encoding`
- `refactor/crawler-interface`

### 1.2 分支创建时机

1. **检查当前分支**：`git branch --show-current`
   - 如果已在功能分支上 → 沿用当前分支
   - 如果在 `main`/`master` 上 → 创建新分支

2. **生成分支名**：根据 1.1 规范自动生成，展示给用户确认

3. **实际创建**：
   ```bash
   git checkout -b {type}/{short-desc}
   ```

---

## Step 2: Commit 规范

### 2.1 Commit Message 格式

```
<type>(<scope>): <description>
```

| 字段 | 说明 |
|------|------|
| `<type>` | `feat` / `fix` / `refactor` / `perf` / `test` / `docs` / `chore` |
| `<scope>` | 变更所在的模块/包，如 `crawler`、`exporter`、`config` |
| `<description>` | 英文，简洁描述变更内容 |

**示例**：
- ✅ `feat(crawler): add bilibili video search API`
- ✅ `fix(exporter): handle special characters in CSV output`
- ✅ `refactor(crawler): extract common HTTP client interface`
- ✅ `test(crawler): add unit tests for bilibili search`
- ❌ `fix bug` — 缺少 scope 和描述
- ❌ `修复了一个问题` — 不要用中文

### 2.2 Commit 禁止行为

| ❌ 禁止 | ✅ 正确做法 |
|--------|-----------|
| 空 commit message | 必须有描述 |
| 临时 message（如 `wip`、`tmp`） | 用正式的描述 |
| 中文 commit message | 英文 |

---

## Step 3: 提交前检查清单

### 3.1 文件完整性检查

```bash
git status
```

**必须确认**：

- [ ] `doc/design/<feature>/spec.md` 已 `git add`（如存在）
- [ ] `doc/design/<feature>/tasks.md` 已 `git add`（如存在）
- [ ] 没有遗漏的代码文件未暂存
- [ ] 没有不应该提交的临时文件

### 3.2 代码质量检查

- [ ] `go build ./...` 编译通过
- [ ] `go vet ./...` 无警告
- [ ] `go test ./...` 测试通过（如有测试）
- [ ] AI CR 通过（`workflow-code-review` 无 P0 / P1 意见）

### 3.3 执行提交

```bash
git add <changed_files>
git commit -m "<type>(<scope>): <description>"
git push -u origin {branch_name}
```

---

## Step 4: 完成提示

```
✅ 代码提交完成

- 分支：{branch_name}
- Commit：{commit_hash}

开发阶段完成 🎉
```

---

## 反模式

| ❌ 错误做法 | ✅ 正确做法 |
|------------|-----------|
| 在 `main`/`master` 上直接 commit | 创建功能分支后再工作 |
| commit message 不带 type/scope | 严格遵守 `<type>(<scope>): <desc>` 格式 |
| 提交前不检查 docs 文件 | 执行 `git status` 确认 spec.md/tasks.md 已纳入 |
| 在提交里混入不相关改动 | 一个提交只包含一个功能的改动 |
