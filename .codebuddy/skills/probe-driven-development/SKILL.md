---
name: probe-driven-development
description: 探针驱动开发方法论。当用户提及"探针反馈开发方法"、"探测反馈开发方法"或"数据驱动开发方法"时手动触发。指导爬虫开发采用"探测→观察→编码"的数据驱动模式。
---

# 探针驱动开发方法论（Probe-Driven Development）

## 核心原则：数据驱动，先探测后编码

爬虫开发**必须**采用"探测→观察→编码"的数据驱动模式，**禁止**采用"设计→编码→调试"的瀑布模式。

**根因**：目标网站的实际行为（API URL、翻页机制、渲染方式）无法从文档或经验可靠推断，必须通过实测获取真实数据后再编程。

---

## 强制流程

### Phase 1: 探测（Probe）

为每个新平台/新页面编写探测测试（probe test），在真实浏览器中：
1. 打开目标页面，dump 所有网络请求和响应
2. 检查 SSR 全局变量（`window.__pinia`、`__INITIAL_STATE__` 等）
3. 记录实际的 API URL、请求参数、响应结构
4. 验证翻页机制（URL 参数 vs 按钮点击 vs 滚动加载）

**产出**：真实的 API URL 列表、响应 JSON 样本、页面渲染方式（SSR/CSR）、翻页机制

### Phase 2: 观察与反向推导

基于探测数据：
1. 确认目标数据存在于哪些 API 响应或 SSR 变量中
2. 记录实际的 URL pattern（用于拦截规则），**必须从真实请求中提取**
3. 记录实际的分页参数（ps、pn 等），**从 API 响应中读取，不要硬编码猜测值**
4. 记录实际的 JSON 结构，与 types.go 中的类型定义对比
5. 验证翻页是否真正生效（对比不同页的数据是否不同）

### Phase 3: 编码与测试循环

基于观察结果编写代码，进入快速迭代循环。

**→ 衔接 `feedback-driven-development`（反馈开发方法）**：Phase 2 完成后，调用反馈开发方法进入"修改→编译→运行→分析→修改"的迭代循环。详见该 Skill 的完整流程规范。

---

## 禁止行为

| ❌ 禁止 | ✅ 正确 |
|---------|--------|
| 从文档/经验推测 API URL pattern | 从探测数据中提取真实 URL |
| 硬编码 page size 等参数 | 从 API 响应中动态读取，或从探测数据中确认 |
| 假设翻页机制（URL 参数 vs 点击） | 先探测验证翻页是否生效（对比不同页数据） |
| 未查阅文档就使用第三方库 API | 先确认 API 语义（参数含义、返回值） |
| 在 spec.md 中写未经验证的技术假设 | 标注"待验证"，实测后更新 |

---

## 探测测试模板

每个平台的 `cmd/probe/` 目录下应包含探测工具，提供：
- 打开目标页面并 dump 所有网络请求的能力
- 检查 SSR 全局变量的能力
- 快速验证单个功能完整流程的能力
- 验证翻页机制和数据正确性的能力

---

## 实测经验记录

### Bilibili（2026-03-27 实测）

| 发现 | 原始假设 | 实测结果 |
|------|---------|---------|
| 搜索页渲染方式 | API 拦截 | SSR 渲染（Pinia），API 拦截无效，需用 `window.__pinia` 提取 |
| 用户信息 API URL | `/x/space/acc/info` | `/x/space/wbi/acc/info`（多了 `wbi/` 路径段） |
| 视频列表翻页 | URL 参数 `?pn=N` 翻页 | URL 参数不影响 SPA 的 API 请求 pn 值，需点击"下一页"按钮 |
| 视频列表 page size | 硬编码 50（后改 25） | 实际 API 返回 `ps=30`，应从响应中读取 |

### YouTube（2026-04-02 实测）

| 发现 | 原始假设 | 实测结果 |
|------|---------|---------|
| 搜索页渲染方式 | 未知 | SSR 渲染（`window.ytInitialData`），526KB，包含完整搜索结果 |
| 搜索页筛选 | 需要点击筛选按钮 | 通过 URL 参数 `sp=` 直接拼接，零额外成本 |
| 搜索页排序 | 用户认为"无排序" | 实测有 Relevance 和 Popularity 两种排序（也通过 `sp=` 控制） |
| 频道元数据 | 全在"更多"弹窗里 | 粉丝数/视频数/handle 在 SSR 中直接可取；注册时间/总播放量/地区需额外 browse API |
| 视频列表翻页 | 未知 | 滚动触发 continuation token，通过 `/youtubei/v1/browse` API 加载更多 |
| 视频列表排序 | 需要点击排序按钮 | 通过 `chipBarViewModel` 的 continuation token 切换，不改变 URL |
| "更多"弹窗数据 | 在 SSR 的 engagementPanel 中 | SSR 中 engagementPanels 为空，需点击描述区域触发 browse API，数据在 `aboutChannelViewModel` 中 |
| 搜索页到底判断 | 未知 | `ytd-continuation-item-renderer` 消失 + `ytd-message-renderer` 出现 "No more results" |
| 组合筛选参数 | 可以拼接 sp 值 | 不能拼接，必须从已筛选页面的 filter dialog 中提取组合后的 sp 值 |
| Shorts tab 结构 | 与 Videos tab 一致 | 外层一致（richGridRenderer），但内容渲染器不同：Shorts 用 `shortsLockupViewModel`（无发布时间、无时长） |
