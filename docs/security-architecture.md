# TokenDance Chat 安全架构

最后更新：2026-10-21　适用：`backend/security`、`backend/handler`、`nginx/`、部署

## 目标与非目标

目标是让一个公开聊天站能承受常见的自动化滥用：撞库/爆破、注册机器人、HTTP 与
WebSocket 洪泛、资源耗尽、代理头伪造。非目标是抵抗 Tbps 级流量——那属于 Cloudflare
的网络层，源站只负责"只剩正常流量之后仍然安全"。

## 信任边界

```
Internet
  │  L3/L4 DDoS、WAF、Bot 规则、TLS
  ▼
Cloudflare  ── CF-Connecting-IP / X-Forwarded-For
  │  origin 仅接受 Cloudflare（$is_cloudflare_proxy）
  ▼
主机 nginx  ── 443, limit_req / limit_conn, 超时
  │  loopback:<backend-port> (docker-proxy)；X-Real-IP=CF-Connecting-IP
  ▼
Go (td-chat 容器) ── 可信代理校验、按 IP/账号限流、Turnstile、会话
  ▼
PostgreSQL（生产，运行角色无 DDL）或 SQLite（单机，只有本机可写）
```

关键不变量：

- 转发头只有在直连对端属于 `CHAT_TRUSTED_PROXY_CIDRS` 时才可信；否则一律用 `RemoteAddr`。
- 浏览器同源必须同时匹配 scheme 和 Host。nginx 覆盖
  `X-Forwarded-Proto $scheme`；该头同样只在可信代理对端下生效。
- `CF-Connecting-IP` 优先，其次 `X-Real-IP`，最后才从右往左解析 `X-Forwarded-For` 并跳过可信段。
- 源站端口只绑定回环；nginx 对非 Cloudflare 来源直接 403。

## 威胁与控制矩阵

| 威胁 | 主要控制 | 备份控制 |
|---|---|---|
| 撞库 / 密码喷洒 | Turnstile（每次登录）+ 每账号/每 IP 失败退避 | bcrypt cost 12、统一错误文案、未知用户等时延 |
| 注册机器人 | 邀请码 + Turnstile | 每 IP 5 次/分钟认证请求上限 |
| HTTP 洪泛 | Cloudflare 速率规则与 WAF | nginx `limit_req`、Go 每 IP 窗口限流 |
| WebSocket 连接洪泛 | nginx `limit_conn`（每 IP 4 条） | 全局 100 连接、每 IP 50 次/10s 升级、8 KiB 帧上限、5 条/秒消息 |
| 慢速攻击 | nginx header/body 超时 | Go `ReadHeaderTimeout`、`ReadTimeout`、`MaxHeaderBytes` |
| 请求体撑爆内存 | nginx `client_max_body_size` | `BodyLimitMiddleware`（JSON 16 KiB，Emoji 128 KiB，图片上传 6 MiB 外层；处理器再按游客 2 MiB / 登录 5 MiB、4 MP 校验；全实例只同时解码一张图片） |
| 代理头伪造 | 可信 CIDR + 头部优先级 | 单测覆盖伪造前缀与不可信对端 |
| 会话伪造 | HMAC-SHA256、常量时间比较、7 天过期 | 签名密钥只来自 `CHAT_SESSION_SECRET`；cookie 为 `HttpOnly`、默认 `Secure`、`SameSite=Lax` |
| 账号枚举 | 登录统一返回 `INVALID_CREDENTIALS`；未知用户执行 dummy bcrypt | 注册由邀请码约束 |
| LLM 成本滥用 | 每人每会话 3 秒冷却、单连接同时一个回复、4096 输出上限、3 轮工具 | 每 IP REST 限额 |
| 链接预览 SSRF | 仅 HTTPS、私网/环回/链路本地拒绝、拨号前二次校验 | 响应体上限与超时 |

## Account OIDC 认证

TokenDanceChat 是 confidential relying party。浏览器只持有 Chat 自己的
`tdchat_session` cookie；Account access token、refresh token 和 ID token 不经过前端。

1. `GET /api/oidc/login` 生成一次性 state、nonce 和 PKCE S256 verifier，然后跳转 Account authorization endpoint。
2. Account 完成登录和授权后回到 `/api/oidc/callback`。Chat 先校验 RFC 9207 `iss`，再消费 state。
3. 后端以 `client_secret_basic` 兑换授权码，验证 ID Token 的 RS256、`iss`、`aud`、`azp`、`exp`、`iat` 和 `nonce`。
4. 后端请求 UserInfo，并确认其 `sub` 与 ID Token 一致。`name`、`picture` 和可选的 `preferred_username` 用于初始 Chat profile。
5. Chat 以 `sub` 为稳定键写入 `oidc_users`，读取该 `sub` 当前 logout
   generation，签发绑定 `sub` + generation 的 `tdchat_session` cookie，然后
   重定向到 `/?oidc_success=1`。

Account `sub` 是身份键，邮箱和 handle 都不是。首次分配的 Chat handle 保存在
SQLite，后续登录不会因为 `preferred_username` 变化而改号。不会按邮箱自动合并
账号。

OIDC 启用时，本地密码、注册和邀请处理器返回 404。保留它们只为无 OIDC 的本地
开发和测试，不构成生产第二登录面。OIDC 模式还会拒绝此前签发的本地 Chat
session cookie；本地 cookie 不会因开关切换而继续成为第二条入口。

### Account 退出与 back-channel

Chat 有两个明确不同的退出操作：

- `离开聊天` 调 `POST /api/logout`，只清 Chat session；
- `退出 TokenDance Account` 调 `POST /api/logout/account`，清 Chat session 后
  返回 Account discovery 的 end-session URL，由 Account host-only cookie 完成
  全局撤销。Chat 同时写入短时 HttpOnly state cookie，Account 回跳后由
  `/api/logout/account/callback` 校验并消费，避免把未绑定的回跳当作本浏览器
  完成的退出。

两个 POST 都要求精确同源；有效 session 还要求 `X-CSRF-Token`。CSRF token 由
session 签名 nonce 经 HMAC 派生，只通过 `no-store` 的 `/api/session` 返回，
不暴露 session nonce。Account 全局退出 URL 不含 Account ID/access/refresh
token，只含公开 `client_id`、精确登记的 post-logout URI 和随机 state。state
不进入 URL 可读的前端存储，只与 HttpOnly 同源 cookie 做常量时间匹配。

Account 的 back-channel Logout Token 是另一个权威通道。Chat 只接受 8 KiB 内
严格 `application/x-www-form-urlencoded` 的单个 `logout_token`，验证 RS256、
`iss`、`aud`、`exp`、`iat`、`jti`、`sub` 和唯一标准 event，不接受 `sid`、
`nonce`、非空 event。按 `sub` 推进 SQLite 持久化 generation：

- REST session 读取和 WS upgrade 校验 generation；
- WS join 与 Hub register 再次校验，防止升级后、join 前的退出竞态；
- 已加入的 live WS 先收 typed `account_logout`，写协程刷出后再关闭连接；
- 同一 JTI + 完全相同 claims 幂等，同 JTI 不同 claims 拒绝；
- Account push 延迟/丢失不改变本地 cookie TTL 上限，也不把 push 当作唯一真值。

## 本地密码认证（仅无 OIDC 开发环境）

登录 `/api/login`：

1. `AuthAllow(ip)`：每 IP 5 次/分钟。
2. Turnstile `siteverify`：token 单次有效 300 秒；校验 `action` 与 `hostname`；
   每次请求携带独立 `idempotency_key`。
3. `LoginGuard.Allow(username, ip)`：同一账号或同一来源处于退避期时返回 429 + `Retry-After`。
4. `Store.VerifyUser`：bcrypt；未知用户也执行一次 dummy bcrypt，避免时延泄漏。
5. 成功：清空该账号与来源的失败计数，签发 HMAC 会话 token；失败：计入退避。

退避阶梯（连续失败）：<5 次无延迟；5–7 次 30 秒；8–11 次 2 分钟；≥12 次 5 分钟。
选择"有上限的延迟"而不是永久锁号，避免攻击者用错误密码把真实用户永久锁死。

注册 `/api/register` 额外要求邀请码；密码长度 8–72 字节（bcrypt 上限）。

## Turnstile 契约

- Secret 只存在于服务端环境变量；缺失即关闭校验（仅限本地/测试）。
- 生产设置 `CHAT_TURNSTILE_REQUIRED=true`：Secret 为空时进程直接拒绝启动，不会静默关闭校验。
- 生产必须设置 `CHAT_TURNSTILE_SECRET`、`CHAT_TURNSTILE_HOSTNAMES`、`CHAT_TURNSTILE_ACTION=login`。
- `timeout-or-duplicate` → `403 TURNSTILE_EXPIRED`，前端需要重置 widget 重新挑战。
- 网络错误 / 非 200 / 无法解析 → `503 VERIFICATION_UNAVAILABLE`，fail closed。
- 其它失败 → `403 TURNSTILE_FAILED`。
- 永不记录 token 或 secret。

## 限流矩阵

| 桶 | 维度 | 预算 | 超限响应 |
|---|---|---|---|
| `api` | IP | 30 次/分钟（`CHAT_API_RATE_LIMIT_PER_MINUTE` 可调） | 429 + `Retry-After: 60` |
| `auth` | IP | 5 次/分钟（登录/注册/邀请） | 429 + `Retry-After: 60` |
| `oidc` | IP | 20 次/分钟（约 4 次完整跳转） | 429 |
| `oidc-backchannel` | Account 来源 IP | 120 次/分钟；支持 batch 20 / 30s 和重试 | 429 |
| `ws` | IP | 50 次升级/10 秒 | 429 |
| 登录退避 | 账号 + IP | 见上表阶梯 | 429 + `Retry-After` |
| 消息 | 连接 | 5 条/秒，单条 2000 字符 | 丢弃并记录 |
| 消息跨连接 | IP（IPv6 /64） | 80 条/10 秒 | WS 错误提示并记录 |
| TokenBot | 用户 + 会话 | 3 秒 | 公共静默 / 私聊提示 |
| TokenBot 每日 | 用户 / IP / 实例 | 50 / 200 / 800 次 + 300 万 tokens | 仅发起人提示 |
| TokenBot 并发 | 实例 | 4 | 繁忙提示 |

限流状态在进程内，适用于单实例。若改为多副本，必须换成共享后端（Redis）或把限额下沉到
Cloudflare；不能在多副本下继续使用进程内计数并声称全局生效。

## Cloudflare 边缘

Zone 是 Free 计划，只有 **1 条** rate limiting 规则，且只支持 `eq`/`contains` 这类
基础表达式（`matches` 属于付费能力）。因此这条规则不按"每个端点一条"设计，而是集中保护
最贵的凭据与 WebSocket 升级面：

- 覆盖 Account 的授权入口，以及 OIDC 关闭时才存在的 Chat `/api/login`、`/api/register`、
  `/api/invite/generate`、`/api/oidc/*`、`/ws`。
- 阈值 20 次 / 10 秒，按 `ip.src` + `cf.colo.id` 计数，命中后 block 10 秒。
- 一般 `/api` 流量由 Cloudflare L7 DDoS 托管规则、nginx `limit_req`/`limit_conn`、
  Go 进程内限流三层共同处理。
- 验证方式：`firewallEventsAdaptive` 查询应出现 `source: "ratelimit"` 且
  `ruleId` 指向该规则；只看 429 状态码无法区分 nginx、Go 和 Cloudflare。

## 模块布局

| 位置 | 职责 |
|---|---|
| `backend/security/proxy.go` | 可信代理判断与客户端 IP 解析 |
| `backend/security/ratelimit.go` | 按桶/键的窗口限流与过期清理 |
| `backend/security/login_guard.go` | 账号 + 来源的失败退避 |
| `backend/security/turnstile.go` | Siteverify 客户端、错误分类 |
| `backend/handler/ratelimit.go` | HTTP 适配：桶预算、中间件、环境变量 |
| `backend/handler/turnstile.go` | 从环境构建 verifier，注入请求 IP |

`security` 包不 import `handler`，也不依赖 `hub`/`store`，可单独测试。

## 配置

| 变量 | 生产值 | 作用 |
|---|---|---|
| `CHAT_TRUSTED_PROXY_CIDRS` | `127.0.0.1/32,192.168.32.0/20` | docker-proxy / nginx 直连段 |
| `CHAT_TURNSTILE_SECRET` | secret | 关闭即失去反机器人 |
| `CHAT_TURNSTILE_HOSTNAMES` | `chat.tokendancelab.com` | 防止 token 被搬到其它站点 |
| `CHAT_TURNSTILE_ACTION` | `login` | 与 widget 的 `action` 一致 |
| `CHAT_TURNSTILE_REQUIRED` | `true` | Secret 缺失时启动失败，防止静默降级 |
| `CHAT_SESSION_SECRET` | 稳定随机值 | 会话签名；OIDC 开启时缺失会在打开数据库前拒绝启动，变更会使所有会话失效 |
| `CHAT_COOKIE_SECURE` | `true` | 禁止 HTTPS 页面把 session cookie 发到明文 HTTP；仅 issuer 与 callback 都是 loopback 的本地开发才可关闭 |
| `CHAT_OIDC_ISSUER` | `https://account.tokendancelab.com` | 唯一 production issuer |
| `CHAT_OIDC_CLIENT_ID` | confidential client id | Chat 的对端身份 |
| `CHAT_OIDC_CLIENT_SECRET` | secret | 只由后端用于 HTTP Basic token exchange |
| `CHAT_OIDC_REDIRECT_URI` | `https://chat.tokendancelab.com/api/oidc/callback` | 必须与 Account client 精确匹配 |
| `CHAT_OIDC_POST_LOGOUT_REDIRECT_URI` | `https://chat.tokendancelab.com/api/logout/account/callback` | 必须与 Account client 精确匹配；固定为无 query 的后端回调，省略时从 callback origin 派生 |
| `CHAT_API_RATE_LIMIT_PER_MINUTE` | 默认 30 | 每 IP REST 预算 |

Docker 网络重建后网关 CIDR 可能变化；用 `docker network inspect` 检查 compose
实际使用的网络。

## 观测

- 每条被拒绝的请求都有结构化日志：`rate limit`、`turnstile`、`login backoff`。
- 登录成功/失败只记录用户名、来源 IP、结果；不记录密码、token、secret。
- 建议在 Cloudflare 与主机侧同时观察 429/403 曲线；只有 Go 侧 429 上升而 CF 侧平稳，
  说明攻击已穿透边缘，需要收紧 CF 规则。

## 部署清单

1. `go test ./...`、`pnpm test`、E2E 全绿。
2. 生产 compose 注入 `CHAT_TRUSTED_PROXY_CIDRS`。
3. nginx：`limit_req_zone`/`limit_conn_zone`、`X-Real-IP $http_cf_connecting_ip`、
   `X-Forwarded-Proto $scheme`、
   `/api/login` 与 `/api/register` 独立限速、`/ws` 连接上限。
4. `nginx -t` 通过后再 reload；保留配置备份。
5. 用无效 Turnstile token 调 `/api/login`，必须返回 403/503 而不是 401。
6. 用同一 IP 连续请求认证接口，必须出现 429 + `Retry-After`。

## 仍待决策

- MFA / Passkey：OIDC 模式由 TokenDance Account 提供；Chat 不复制第二套 Passkey/MFA 状态。
- 密码泄露名单（k-anonymity API）与注册时的弱口令拦截。
- 多副本时把限流迁移到 Redis 或全部下沉 Cloudflare。
- Cloudflare WAF 自定义规则以代码化（Terraform/API）管理，避免只存在于控制台。
