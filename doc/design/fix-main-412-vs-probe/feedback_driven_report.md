# 反馈驱动开发记录：修复主程序 412 问题（主程序 vs 探针差异）

## 1. 背景

### 1.1 问题描述

探针（probe_412_test.go）模拟了 20 个 author 的请求，即使零间隔也不触发 412。但主程序（crawler）运行相同的 20 个 author 时，在 Stage 1 中频繁触发 412（16 次 412，5 个 author 失败）。

### 1.2 根因分析

**主程序 vs 探针的关键差异**：

| 维度 | 探针 | 主程序 |
|------|------|--------|
| 每个 author 的 API 请求数 | 2（info + video_page1） | 2 + N_pages（info + video_page1 + 翻页点击） |
| 翻页操作 | ❌ 无 | ✅ 有，每个 author 翻 1-25 页 |
| 翻页间隔 | N/A | 800ms（paginationInterval） |
| 实际 API 请求总量（20 author） | ~40 | ~200+（取决于每个 author 的视频数） |

**412 触发模式**（从 test_run.log 分析）：
- 首个 412 出现在 `pn=3` 的翻页请求（02:29:57），此时 Stage 1 已运行约 41 秒
- 随后 412 蔓延到新 author 的 `pn=1` 初始导航
- 3 个 worker 同时翻页，每个 worker 内部翻页间隔仅 800ms，远小于 worker 间的 2500ms

**核心问题**：翻页操作（pagination click）的请求密度太高，3 个 worker 同时翻页时，实际 API 请求频率远超 requestInterval 的控制范围。

### 1.3 修复方案

1. **增大翻页间隔**：将 `paginationInterval` 从 800ms 提升到与 `requestInterval` 相当的水平
2. **翻页时也检查 global cooldown**：当前翻页循环不检查 cooldown，412 触发后其他 worker 的翻页仍在继续
3. **翻页 412 应触发 author 级别重试**：当前翻页 412 只是 break 循环（丢失后续页数据），不触发重试

### 1.4 验证目标

主程序能以 "生化危机9" 关键词 + 50 个 author 完成 Stage 1，无 412 失败。

### 1.5 运行命令

```bash
cd /data/workspace_vscode/fast_web_meta_crawler && go build -o crawler . && ./crawler --platform bilibili --keyword "生化危机9" --stage 1 --limit 50 2>&1 | tee /tmp/main_412_fix.log
```

---

## 2. 迭代过程

（待填充）
