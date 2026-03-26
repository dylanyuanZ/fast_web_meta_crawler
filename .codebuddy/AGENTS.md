# fast_web_meta_crawler

## 项目概述

fast_web_meta_crawler 是一个 Go 语言编写的视频平台元数据爬虫工具，用于爬取 bilibili、YouTube、Instagram 等平台的视频元数据（点赞数、收藏数、转发数等），输出 CSV 格式结果。

## 目录结构

| 目录 | 说明 |
|------|------|
| `src/` | 源代码目录 |
| `build/` | 编译产物目录 |
| `doc/` | 文档目录（spec.md、tasks.md 等） |
| `conf/` | 配置目录 |

## 技术栈

- **语言**: Go
- **领域**: Web 爬虫、HTTP 协议、数据解析、并发编程
- **输出格式**: CSV

## AI 工作流

本项目配置了完整的 AI 辅助开发工作流：

```
需求澄清 → 系统设计 → 代码生成 → 测试生成 → 代码评审 → 代码提交
```

详见 `.codebuddy/skills/` 和 `.codebuddy/rules/` 目录。
