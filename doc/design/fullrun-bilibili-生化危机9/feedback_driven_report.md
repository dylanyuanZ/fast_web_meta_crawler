# 反馈驱动开发记录：全量跑测 bilibili + "生化危机9"

## 1. 背景

### 1.1 问题描述

验证 crawler 工具在 bilibili 平台 + "生化危机9" 关键字 + 不设 limit 条件下，能否完整执行成功并生成完整的 CSV 记录。

此前已通过反馈驱动开发修复了 412 阻塞问题（详见 `doc/design/feedback-driven-412-fix/report.md`），并在 "炸鸡" 关键字 + limit 20 条件下验证通过。本次是首次使用新关键字进行全量跑测。

### 1.2 验证目标

1. **执行成功**：程序正常退出（exit code 0），无 FATAL 错误
2. **Stage 0 完整**：搜索页面爬取完成，生成 videos.csv，包含搜索到的所有视频
3. **Stage 1 完整**：博主详情爬取完成，生成 authors.csv，每个博主有完整的统计数据（粉丝数、视频数、平均播放量等）
4. **CSV 记录完整**：authors.csv 中的记录数 = Stage 0 去重后的博主数（或接近，允许少量因风控失败的博主）

### 1.3 运行命令

```bash
cd /data/workspace_vscode/fast_web_meta_crawler
go run cmd/crawler/main.go -platform bilibili -keyword "生化危机9" -stage all -config conf/config.yaml
```

### 1.4 配置参数

| 参数 | 值 |
|------|-----|
| platform | bilibili |
| keyword | 生化危机9 |
| limit | 不设（全量） |
| concurrency | 3 |
| request_interval | 2500ms |
| max_search_page | 50 |
| max_video_per_author | 1000 |
| max_consecutive_failures | 10 |
| headless | true |

---

## 2. 问题分析

（首轮循环分析后填写）

---

## 3. 迭代过程

（每轮循环末尾追加）

---

## 4. 修复总结

（Step 4 结束时填写）
