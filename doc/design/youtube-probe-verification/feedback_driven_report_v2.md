# YouTube 探针验证报告 V2 —— 深度验证

## 1. 背景

### 问题描述
在第一轮探针验证（feedback_driven_report.md）中，已确认 YouTube 的 SSR 模式、字段映射、筛选参数等基础信息。本轮需要深入验证 3 个具体问题：
1. 搜索页面/作者页面如何判断"滚动到底部"？
2. "更多"弹窗的数据（注册时间、其他平台链接、播放量、地区）具体怎么请求？
3. Shorts tab 的数据结构是否与 Videos tab 一致？

### 验证目标

| # | 验证目标 | 验证方式 |
|---|---------|---------|
| 1 | 搜索页面滚动到底部的判断机制 | 搜索"天齐锂业" + 筛选长视频 + 本周，数据极少，反复滚动直到底部，检查 continuation token 和 DOM 变化 |
| 2 | "更多"弹窗数据的请求方式 | 从 SSR 中提取 engagementPanel 的 continuation token，构造 browse API 请求，解析返回数据 |
| 3 | Shorts tab 数据结构 | 打开 @BrunoMars/shorts，对比 Videos tab 的 richGridRenderer 结构 |

### 运行命令
```bash
# 验证目标 1：搜索页面滚动到底部
go test -v -run TestProbeYouTubeScrollToBottom -timeout 180s ./probe/

# 验证目标 2：更多弹窗数据请求
go test -v -run TestProbeYouTubeAboutPanel -timeout 120s ./probe/

# 验证目标 3：Shorts tab 数据结构
go test -v -run TestProbeYouTubeShortsTab -timeout 120s ./probe/
```

---

## 2. 迭代过程

### 第 1 轮：验证搜索页面滚动到底部的判断机制

**修改内容**：新增 `TestProbeYouTubeScrollToBottom` 测试函数。搜索"天齐锂业" + Videos 筛选 + This week 筛选，数据极少，反复滚动检测底部。

**运行结果**：✅ PASS（37.6s）

**关键发现**：

1. **组合筛选的 sp 参数**：当 Videos 筛选已生效时，再选 "This week"，YouTube 会返回一个**组合后的 sp 值** `EgQIAxAB`（而非两个独立的 sp 拼接）。这意味着筛选参数是 protobuf 编码的组合值，**必须从已筛选页面的 filter dialog 中提取**。

2. **初始状态**：`estimatedResults: 18`，初始加载 20 个 item（含 2 个 `searchPyvRenderer` 广告），有 `continuationItemRenderer`。

3. **滚动到底部的判断机制**（核心结论）：

| 指标 | Scroll 1 | Scroll 2（到底） |
|------|----------|-----------------|
| `videoRenderers` | 18 | 18（不变） |
| `continuationItems` | 1 | **0**（消失） |
| `messageRenderers` | 0 | **1**（出现） |
| `messageTexts` | [] | **["No more results"]** |
| `continuationVisible` | true | **false** |

**结论：判断搜索页面到底部有 3 种互补方式**：
- ✅ **方式 A（推荐）**：检查 `ytd-continuation-item-renderer` 数量从 >0 变为 0
- ✅ **方式 B（辅助）**：检查 `ytd-message-renderer` 出现且文本包含 "No more results"
- ✅ **方式 C（兜底）**：连续两次滚动后 `videoRenderers` 数量不变

**程序实现建议**：
```
loop:
  scroll → wait 3s → check DOM
  if continuationItems == 0 → break (到底了)
  if videoCount == prevVideoCount → retry once → still same → break
```

4. **组合筛选参数表**（从实测中提取，Videos 已选中时的组合值）：

| 组合 | sp 值 |
|------|-------|
| Videos + Today | `EgQIAhAB` |
| Videos + This week | `EgQIAxAB` |
| Videos + This month | `EgQIBBAB` |
| Videos + This year | `EgQIBRAB` |
| Videos + Under 3 min | `EgQQARgE` |
| Videos + 3-20 min | `EgQQARgF` |
| Videos + Over 20 min | `EgQQARgC` |
| Videos + Popularity | `CAMSAhAB` |

### 第 2 轮：验证"更多"弹窗数据的请求方式

**修改内容**：新增 `TestProbeYouTubeAboutPanel` 测试函数。打开 @BrunoMars 频道，提取 engagementPanel，点击 "...more" 触发 browse API，解析返回数据。

**运行结果**：✅ PASS（14.7s）

**关键发现**：

1. **SSR 中没有 `engagementPanels` 数据**：初始 SSR 的 `engagementPanels` 为空（`{}`），这与 V1 报告中的推测不同。About 数据**不在初始 SSR 中**。

2. **触发方式**：点击频道描述区域（`yt-description-preview-view-model`）即可触发 browse API 请求。

3. **browse API 响应结构**：
```
onResponseReceivedEndpoints[0]
  .appendContinuationItemsAction
    .continuationItems[0]
      .aboutChannelRenderer
        .metadata
          .aboutChannelViewModel
```

4. **`aboutChannelViewModel` 完整字段**（从真实数据中提取）：

| 字段 | 示例值 | 说明 |
|------|--------|------|
| `description` | "Bruno Mars is a 16x GRAMMY®..." | 频道介绍 |
| `subscriberCountText` | "43.3M subscribers" | 粉丝数 |
| `videoCountText` | "121 videos" | 总视频数 |
| `viewCountText` | "25,113,856,444 views" | **总播放量** ✅ |
| `joinedDateText.content` | "Joined Sep 19, 2006" | **注册时间** ✅ |
| `country` | "" (Bruno Mars 未设置) | **地区** ✅（有的博主有） |
| `canonicalChannelUrl` | "http://www.youtube.com/@brunomars" | 频道 URL |
| `channelId` | "UCoUM-UJ7rirJYP8CQ0EIaHA" | 频道 ID |
| `links` | 6 个 `channelExternalLinkViewModel` | **其他平台链接** ✅ |
| `artistBio` | (有值) | 艺术家简介 |
| `displayCanonicalChannelUrl` | (有值) | 显示用 URL |

5. **链接列表**（从真实数据中提取）：

| # | title | link |
|---|-------|------|
| 1 | brunomars.com | brunomars.com |
| 2 | Store | brunomars.lnk.to/officialstore |
| 3 | TikTok | tiktok.com/@brunomars |
| 4 | Instagram | instagram.com/brunomars |
| 5 | X | x.com/brunomars |
| 6 | Facebook | facebook.com/brunomars |

**程序实现方案**：
```
方案 A（推荐）：点击描述区域触发 browse API
  1. Navigate → youtube.com/@author
  2. 等待加载 → 提取 SSR 中的基础信息（姓名、粉丝数等）
  3. 点击 yt-description-preview-view-model
  4. 拦截 /youtubei/v1/browse 响应
  5. 解析 aboutChannelViewModel → 获取注册时间、总播放量、地区、链接

方案 B（备选）：直接从 SSR 中提取 continuation token，构造 browse API 请求
  需要进一步验证 token 的提取路径
```

### 第 3 轮：验证 Shorts tab 数据结构

**修改内容**：新增 `TestProbeYouTubeShortsTab` 测试函数。打开 @BrunoMars/shorts，分析数据结构，然后打开 /videos 对比。

**运行结果**：✅ PASS（19.1s）

**关键发现 —— Shorts vs Videos 对比表**：

| 维度 | Videos tab | Shorts tab | 是否一致 |
|------|-----------|------------|---------|
| 外层容器 | `richGridRenderer` | `richGridRenderer` | ✅ 一致 |
| 列表项类型 | `richItemRenderer` | `richItemRenderer` | ✅ 一致 |
| **内容渲染器** | `videoRenderer` | **`shortsLockupViewModel`** | ❌ **不同** |
| 排序 chips | Latest/Popular/Oldest | Latest/Popular/Oldest | ✅ 一致 |
| header | `chipBarViewModel` | `chipBarViewModel` | ✅ 一致 |
| 有 continuation | ✅ | 未检测到（仅 10 条） | 待验证 |
| 初始加载数 | 31 | 10 | 不同 |

**Shorts 的 `shortsLockupViewModel` 字段**：

| 字段 | 说明 | 示例 |
|------|------|------|
| `entityId` | 实体 ID | "shorts-shelf-item-GkwAxk-0Q3I" |
| `accessibilityText` | 无障碍文本（含标题+播放量） | "New Video. New Album..., 518 thousand views" |
| `overlayMetadata.primaryText.content` | **视频标题** | "New Video. New Album. New Chapter. #TheRomantic 🌹" |
| `overlayMetadata.secondaryText.content` | **播放量** | "518K views" |
| `thumbnailViewModel` | 缩略图 | (有值) |
| `onTap` | 点击导航 | (有值) |

**与 Videos tab 的 `videoRenderer` 对比**：

| 数据 | videoRenderer 路径 | shortsLockupViewModel 路径 |
|------|-------------------|---------------------------|
| 视频标题 | `title.runs[].text` | `overlayMetadata.primaryText.content` |
| 播放量 | `viewCountText.simpleText` | `overlayMetadata.secondaryText.content` |
| 发布时间 | `publishedTimeText.simpleText` | **❌ 无此字段** |
| 视频时长 | `lengthText.simpleText` | **❌ 无此字段**（Shorts 固定短视频） |
| 视频 ID | `videoId` | 需从 `entityId` 或 `onTap` 中提取 |

**结论**：Shorts tab 和 Videos tab 的**外层结构一致**（都是 `richGridRenderer` + `richItemRenderer`），但**内容渲染器完全不同**。Shorts 用 `shortsLockupViewModel`，字段更少（无发布时间、无时长），需要单独的解析逻辑。

---

## 3. 总结

### 所有验证目标结果

| # | 验证目标 | 结果 | 关键结论 |
|---|---------|------|---------|
| 1 | 搜索页面滚动到底部 | ✅ | `continuationItems` 从 1→0 + `messageRenderers` 出现 "No more results" |
| 2 | "更多"弹窗数据请求 | ✅ | 点击描述区域 → 拦截 browse API → `aboutChannelViewModel` 包含全部字段 |
| 3 | Shorts tab 数据结构 | ✅ | 外层一致（richGridRenderer），内容渲染器不同（shortsLockupViewModel vs videoRenderer） |

### 对 V1 报告的修正

| V1 结论 | V2 修正 |
|---------|---------|
| "更多"弹窗数据在 engagementPanel 中 | SSR 中 engagementPanels 为空，数据通过点击触发 browse API 获取 |
| 需要额外 browse API 请求获取注册时间等 | ✅ 确认，但触发方式是点击描述区域，不是手动构造请求 |
| 筛选通过 sp= 参数控制 | ✅ 确认，且**组合筛选需要从已筛选页面提取组合后的 sp 值** |

### 程序实现要点更新

1. **搜索页面滚动**：每次 `scrollBy(0, 3000)` + `sleep(3s)` → 检查 `ytd-continuation-item-renderer` 数量 → 为 0 则到底
2. **频道详情获取**：点击 `yt-description-preview-view-model` → 拦截 `/youtubei/v1/browse` → 解析 `aboutChannelViewModel`
3. **Shorts 解析**：需要独立的解析器，从 `shortsLockupViewModel.overlayMetadata` 提取标题和播放量
4. **组合筛选**：不能简单拼接 sp 值，必须先应用第一个筛选，再从页面提取组合后的 sp 值
