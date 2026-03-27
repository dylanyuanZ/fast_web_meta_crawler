
# Bilibili 反爬策略调试笔记

**日期**: 2026-03-27  
**状态**: 持续更新

> 本文档记录了实际调试中发现的 Bilibili 反爬机制及对应的应对策略，作为后续维护和新平台接入的参考。  
> 从 `spec.md` §4.4 迁移而来，spec.md 保持纯设计文档定位。

---

## 1. 问题现象

| 现象 | 触发条件 | API 响应 |
|------|---------|---------|
| HTTP 412 Precondition Failed | 请求缺少必要的 Cookie（如 `buvid3`） | HTTP 状态码 412，无 JSON body |
| API 业务错误 `-799` | 同一 IP 短时间内请求过于频繁 | `{"code": -799, "message": "请求过于频繁"}` |
| API 业务错误 `-352` | wbi 签名缺失/错误，或 IP 级别风控 | `{"code": -352, "message": "风控校验失败"}` |

## 2. 应对策略

### 2.1 Cookie 自动获取（Cookie Jar + 主页预热）

Bilibili API 要求请求携带有效的 Cookie（至少包含 `buvid3`）。解决方案：

1. **自动预热**：`httpclient.New()` 初始化时，使用 `net/http/cookiejar` 创建 Cookie Jar，然后访问 `https://www.bilibili.com` 主页，自动获取 `Set-Cookie` 中的 `buvid3` 等初始 Cookie
2. **手动配置**：用户可在 `config.yaml` 中配置 `cookie` 字段，粘贴浏览器中的完整 Cookie 字符串（包含 `SESSDATA` 等登录态），登录态请求的风控阈值更高
3. **优先级**：手动配置的 Cookie 优先于自动获取的 Cookie

**如何在 Chrome 中获取 Cookie**：
1. 打开 Chrome，登录 bilibili.com
2. 按 F12 打开开发者工具 → Network 标签
3. 刷新页面，点击任意一个请求
4. 在 Request Headers 中找到 `Cookie` 字段，复制完整值
5. 粘贴到 `config.yaml` 的 `cookie` 字段中

### 2.2 请求头伪装

所有请求携带以下 Headers，模拟真实浏览器行为：

```
User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 ...
Referer: https://www.bilibili.com/
Origin: https://www.bilibili.com
```

### 2.3 请求间隔控制

- Worker Pool 每个 worker 执行完一个任务后，等待 `request_interval`（默认 1200ms）再取下一个任务
- `processOneAuthor` 内部的多个 API 调用之间也加入 `request_interval` 间隔
- 可通过 `config.yaml` 调整间隔大小，在速度和稳定性之间取得平衡

### 2.4 wbi 签名（`x/space/wbi/arc/search` API）

Bilibili 的用户投稿视频列表 API（`/x/space/wbi/arc/search`）需要 wbi 签名参数。实现方案：

1. 从 `/x/web-interface/nav` API 获取 `img_key` 和 `sub_key`（从 `wbi_img.img_url` 和 `wbi_img.sub_url` 的文件名中提取）
2. 使用固定的 `mixinKeyEncTab` 置换表，将 `img_key + sub_key` 拼接后置换生成 `mixin_key`（取前 32 位）
3. 将请求参数按 key 排序，拼接为 query string，附加 `wts`（当前时间戳）
4. 计算 `w_rid = MD5(sorted_query_string + mixin_key)`
5. 将 `w_rid` 和 `wts` 附加到请求 URL

> **注意**：`img_key` 和 `sub_key` 会定期更换，当前实现在首次调用时获取并缓存。如果长时间运行后签名失效，需要重新获取。

### 2.5 API 业务错误重试

在 HTTP 层重试之上，增加了 API 业务层的重试机制：

| 错误码 | 含义 | 重试策略 |
|--------|------|---------|
| `-799` | 请求过于频繁 | 指数退避重试，基础延迟 3s，最多 5 次 |
| `-352` | 风控校验失败 | 同上 |
| 其他非零 code | 业务错误 | 不重试，直接返回错误 |

### 2.6 HTTP 412 可重试

Bilibili 在检测到异常请求时会返回 HTTP 412，这不是标准的客户端错误，而是反爬机制的一种表现。将其归类为可重试错误后，配合指数退避，大部分请求最终能成功。

## 3. 实际效果

| 阶段 | 效果 |
|------|------|
| 阶段 0（搜索） | ✅ 50 页全部成功，0 失败，约 10s 完成 |
| 阶段 1（博主详情） | ✅ 连续 5 次测试均 100% 成功（详见下方 benchmark） |

### Benchmark 测试（2026-03-27）

**测试条件**：20 个博主 × 5 次连续运行（无间隔），每次创建全新 HTTP 客户端。  
**配置**：`concurrency=1`, `request_interval=2500ms`, `max_retries=3`，已配置浏览器 Cookie（含 SESSDATA 登录态）。  
**每个博主请求**：user info + user stat + 1 页视频列表（含 wbi 签名），共 3 个 API 调用。

| Run | 成功 | 失败 | 成功率 | 耗时 | 备注 |
|-----|------|------|--------|------|------|
| 1 | 20 | 0 | 100% | 2m56s | 部分博主触发 1-4 次 `-799` 重试 |
| 2 | 20 | 0 | 100% | 3m38s | 紧接 Run 1，重试次数增多但全部恢复 |
| 3 | 20 | 0 | 100% | 3m36s | 同上 |
| 4 | 20 | 0 | 100% | 3m45s | 最多 4 次重试（指数退避至 24s），仍成功 |
| 5 | 20 | 0 | 100% | 5m35s | 连续压力最大的一轮，重试最多，仍全部成功 |
| **总计** | **100** | **0** | **100%** | — | — |

**关键观察**：
- 所有失败均为 `-799`（请求过于频繁），**未出现 `-352`（风控校验失败）**
- 指数退避重试（3s → 6s → 12s → 24s）非常有效，最多 4 次重试即可恢复
- 连续高压运行下（5 轮无间隔），成功率仍保持 100%
- 配置浏览器 Cookie（含 SESSDATA 登录态）是高成功率的关键因素

### 推荐配置（稳定优先）

```yaml
concurrency: 1               # 单 worker，最安全的反爬配置
request_interval: 2500ms     # 每个 worker 请求间隔（实际会加 ±30% 随机抖动）
http:
  timeout: 15s               # 增大超时
  max_retries: 3
  initial_delay: 2s          # 增大首次重试延迟
  max_delay: 15s
  backoff_factor: 2.0
max_consecutive_failures: 10 # 提高熔断阈值，避免误触发
cookie: "..."                # 强烈建议配置浏览器 Cookie
```

## 4. 进一步优化方向

1. ~~**配置真实浏览器 Cookie**~~：✅ 已实现，在 `config.yaml` 中配置了包含 `SESSDATA` 的登录态 Cookie
2. **代理 IP 池**：在 `httpclient` 层面集成代理轮换，分散请求到不同 IP，从根本上解决 IP 级别风控
3. ~~**wbi 签名 key 定期刷新**~~：✅ 已实现，收到 `-352` 错误时自动清空缓存的 wbi key，下次请求重新获取
4. ~~**请求随机化**~~：✅ 已实现，在 `request_interval` 基础上加入 ±30% 随机抖动（`pool.JitteredDuration`），避免固定节奏被识别为爬虫
5. ~~**Cookie 处理优化**~~：✅ 已实现，手动配置 Cookie 时禁用 cookiejar，避免手动 Cookie 和 jar 管理的 Cookie 冲突

## 5. 涉及的代码文件

| 文件 | 改动内容 |
|------|---------|
| `src/httpclient/client.go` | Cookie Jar + 主页预热 + 请求头 + 412 可重试 |
| `src/config/config.go` | 新增 `cookie` 和 `request_interval` 配置项 |
| `src/pool/pool.go` | Worker 请求间隔 + ±30% 随机抖动（`JitteredDuration`） |
| `src/crawler.go` | `processOneAuthor` 内部 API 调用间隔（带随机抖动） |
| `src/platform/bilibili/wbi.go` | wbi 签名实现 |
| `src/platform/bilibili/types.go` | `apiError` 类型 + 可重试错误码 |
| `src/platform/bilibili/author.go` | `retryOnAPIError` + wbi 签名调用 |
| `src/platform/bilibili/search.go` | `retryOnAPIError` |
| `conf/config.yaml` | 新增配置项 |
