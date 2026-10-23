# TokenDance Account OIDC setup

最后更新：2026-10-21

TokenDanceChat 使用标准 OpenID Connect，通过 TokenDance Account 登录。Chat 是
confidential relying party，浏览器只持有 Chat 自己的 `tdchat_session` cookie。
Account access token、refresh token 和 ID token 不进入浏览器。

开发者契约以 [TokenDance Account OIDC API v1](https://account.tokendancelab.com/developers)
为权威来源。生产 issuer 固定为 `https://account.tokendancelab.com`。

## 1. 注册 client

在 Account 管理面为 Chat 注册一个 confidential client。以下三个 URI 都按
环境精确登记，不能跨环境复用：

| 环境 | Authorization redirect | Post-logout redirect | Back-channel logout |
|---|---|---|---|
| local | `http://localhost:8080/api/oidc/callback` | `http://localhost:8080/api/logout/account/callback` | 不登记；Account 拒绝 loopback/private endpoint，隔离 harness 直接验协议 |
| production | `https://chat.tokendancelab.com/api/oidc/callback` | `https://chat.tokendancelab.com/api/logout/account/callback` | `https://chat.tokendancelab.com/api/oidc/backchannel-logout` |

每个 origin 使用独立 client。不要把 localhost 和生产 callback 混在同一 client
里，也不要复用生产 secret。

申请 scope：

```text
openid profile email
```

注册结果包含公开的 `client_id` 和只展示一次的 client secret。secret 进入部署
secret store，不进入 README、`.env.example`、Git、构建日志或浏览器 bundle。

## 2. 配置服务

本地 OIDC 示例：

```bash
CHAT_OIDC_ENABLED=true
CHAT_OIDC_ISSUER=https://account.tokendancelab.com
CHAT_OIDC_CLIENT_ID=tdc_YOUR_LOCAL_CLIENT_ID
CHAT_OIDC_CLIENT_SECRET=YOUR_LOCAL_CLIENT_SECRET
CHAT_OIDC_REDIRECT_URI=http://localhost:8080/api/oidc/callback
CHAT_OIDC_POST_LOGOUT_REDIRECT_URI=http://localhost:8080/api/logout/account/callback
CHAT_SESSION_SECRET=STABLE_RANDOM_VALUE
CHAT_COOKIE_SECURE=false
```

生产使用：

```bash
CHAT_OIDC_ENABLED=true
CHAT_OIDC_ISSUER=https://account.tokendancelab.com
CHAT_OIDC_CLIENT_ID=tdc_YOUR_PRODUCTION_CLIENT_ID
CHAT_OIDC_CLIENT_SECRET=YOUR_PRODUCTION_CLIENT_SECRET
CHAT_OIDC_REDIRECT_URI=https://chat.tokendancelab.com/api/oidc/callback
CHAT_OIDC_POST_LOGOUT_REDIRECT_URI=https://chat.tokendancelab.com/api/logout/account/callback
CHAT_SESSION_SECRET=STABLE_RANDOM_VALUE
CHAT_COOKIE_SECURE=true
```

OIDC 开启但缺少稳定的 `CHAT_SESSION_SECRET`、issuer、client id、client secret
、redirect URI 或 post-logout redirect URI 时，Chat 在打开数据库前拒绝启动。
未显式配置 post-logout URI 时，后端会从 callback origin 派生
`/api/logout/account/callback`；生产仍应显式配置并核对 Account 登记值。
`CHAT_OIDC_ENABLED` 和
`CHAT_COOKIE_SECURE` 是严格布尔配置，拼写错误不会被当成 `false`。
`CHAT_COOKIE_SECURE` 默认是 `true`；只有 issuer 和 redirect URI 都指向 loopback
的开发环境才允许设为 `false`。

## 3. 登录流程

1. 浏览器访问 `/api/oidc/login`。Chat 创建一次性 `state`、`nonce` 和 PKCE S256
   verifier，然后跳转 Account authorization endpoint。
2. Account 完成用户登录、必要时的身份绑定和 consent。
3. Account 返回 `/api/oidc/callback`，响应包含 authorization code、state 和
   RFC 9207 `iss`。
4. Chat 校验 `iss`，消费 state，再通过后端请求 token endpoint。client 认证使用
   HTTP Basic，请求同时保留 `code_verifier`。
5. Chat 使用 JWKS 验证 ID Token：只接受 RS256，并校验 issuer、audience、
   `azp`、过期时间、签发时间和 nonce。
6. Chat 调用 UserInfo，确认 `sub` 与 ID Token 一致，再同步 `name`、`picture`
   和可选的 `preferred_username`。
7. Chat 以 `sub` 为稳定身份键写入 `oidc_users`，读取该 `sub` 当前 logout
   generation，签发同时绑定 `sub` 和 generation 的 `tdchat_session` cookie，
   重定向到 `/?oidc_success=1`。

`tdchat_session` 的属性是：

| 属性 | 值 |
|---|---|
| Name | `tdchat_session` |
| HttpOnly | `true` |
| Secure | 默认 `true`，仅 loopback 开发可关闭 |
| SameSite | `Lax` |
| Path | `/` |
| Lifetime | 7 天 |

REST 和 WebSocket upgrade 都从该 cookie 绑定身份。WS `join` 只提交昵称，不携带
Account token。游客仍可匿名加入公共房间。

## 4. 身份和资料

Account `sub` 是唯一稳定身份键。Chat 不使用邮箱自动合并账号，也不把
`preferred_username`、昵称或头像 URL 当作身份键。

首次登录时，Chat 按以下顺序选择可读 handle：

1. 合法且非保留的 `preferred_username`；
2. 邮箱 local-part 清洗后的值；
3. `name` 清洗后的值；
4. 从 `sub` 摘要派生 `td_<hash>`。

选出的 Chat handle 只在首次建号时决定，后续登录不会随 Account 改名而漂移。
`name` 和 `picture` 每次成功登录都会刷新。Account 不返回头像时省略 `picture`。
`preferred_username` 可以为空。

## 5. 路由和边界

OIDC 启用时，Chat 注册以下身份路由：

| Route | Purpose |
|---|---|
| `GET /api/oidc/config` | 返回不含 secret 的前端配置 |
| `GET /api/oidc/login` | 创建 state/nonce/PKCE 并跳转 Account |
| `GET /api/oidc/callback` | 校验回调、兑换 code、建立 Chat session |
| `GET /api/logout/account/callback` | 校验并消费发起退出的同浏览器 state，再回跳 Chat 首页 |
| `POST /api/oidc/backchannel-logout` | 接收 Account Logout Token，持久化撤销并断开 live WS |

旧 `/api/oidc/exchange`、`/api/oidc/refresh` 和 `/api/oidc/redeem` 已删除并返回
404。浏览器没有 Account token，也不会把 session token 写入
`localStorage`。

OIDC 启用时，本地 `/api/register`、`/api/login`、`/api/invite/generate` 和
`/api/invite/list` 返回 404。它们只用于没有 OIDC 的本地开发和测试，不是生产
账号入口。

### 本地退出与 Account 全局退出

- `POST /api/logout`：同源 + session 派生 CSRF；只清 Chat cookie。无有效
  session 时仍清过期 cookie，不提供账号存在性 oracle。
- `POST /api/logout/account`：同源 + OIDC session + CSRF；先清 Chat cookie，
  再返回 discovery 中 Account end-session endpoint 构造的顶层导航 URL。
  URL 只含 `client_id`、已登记 `post_logout_redirect_uri` 和随机 `state`，不含
  Account ID/access/refresh token。同一个随机 state 写入 10 分钟有效的
  HttpOnly、SameSite=Lax、无 Domain 的同源 cookie；生产使用
  `__Host-tdchat_logout`。
- Account 完成全局撤销后回跳
  `/api/logout/account/callback?state=...`。Chat 后端要求 state 与 cookie
  唯一值常量时间匹配，并在成功、失败和重放时都消费 cookie；只有成功校验后才
  回跳 `/?oidc_logged_out=1`。
- Account 完成全局撤销后，请求各已登记 client 的 back-channel endpoint。Chat
  以 `sub` 为权威失效本地 session/WS，不尝试解析 Account cookie，也不伪造
  `sid` 粒度。

Logout Token 校验：

- 只接受 `application/x-www-form-urlencoded`、单个 `logout_token`、8 KiB 上限；
- RS256、`iss`、`aud`、`exp`、`iat`、`jti`、`sub` 和唯一标准 event 均校验；
- `sid`、`nonce`、非空 event 或错误 event 一律拒绝；
- 同一 `jti` + 完全相同 claims 幂等返回 204；同 `jti` 不同 claims 返回 400；
- JTI 去重至少覆盖 Account 投递窗口；持久化或 live-session 入队失败返回 503
  让 Account 重试。

OIDC cookie 的 generation 是 durable authority。Account push 可能延迟或丢失，
所以每次 REST session 读取、WS upgrade、WS join 和 Hub register 都要核对当前
generation。back-channel 命中时，live WS 先收到 typed `account_logout`，再关闭；
前端停止重连并回到未登录状态。

## 6. 安全约束

- 生产必须使用 HTTPS，并保持 `CHAT_COOKIE_SECURE=true`。
- client secret 只能放在服务端。
- state 一次性消费，默认 10 分钟过期，并设容量上限。
- JWKS 缓存 5 分钟；遇到未知 `kid` 强制刷新一次，再拒绝 token。
- 未知 `kid` 强制刷新有 30 秒冷却，防止伪造 `kid` 放大为无界出站请求。
- discovery、JWKS、token 和 UserInfo 请求都有 5 秒超时和响应体上限。
- `picture` 只接受无 userinfo、无 fragment 的 HTTPS URL。
- 不在 URL、响应、日志、localStorage 或 Service Worker cache 中持久化 Account
  token。
- OIDC endpoint 有独立 per-IP 限流；公开入口还应经过 Cloudflare、nginx 和 Chat
  进程限流。

## 7. 排查

| 现象 | 检查 |
|---|---|
| 启动报 OIDC setup failed | issuer、client id、secret、redirect URI 是否齐全，discovery 是否可访问 |
| `invalid_issuer` | Account 回调缺少或返回了错误的 RFC 9207 `iss` |
| `invalid_state` | state 是否过期、已被消费，或浏览器是否开了多个并行登录 |
| `token_exchange_failed` | callback、client 类型、secret 和 Account client 注册是否一致 |
| `invalid_token` | JWKS 可达性、issuer、audience、nonce、时钟偏差和签名算法 |
| `invalid_userinfo` | UserInfo 是否可达，`sub` 是否与 ID token 一致，`picture` 是否为 HTTPS |
| 本地登录后又跳回未登录 | HTTP 开发是否设置 `CHAT_COOKIE_SECURE=false` |
| Account 已退出但 Chat 仍登录 | 检查 back-channel URI、discovery/JWKS、Account logout outbox 和 Chat generation |
| Account 全局退出返回 `invalid_request` | `CHAT_OIDC_POST_LOGOUT_REDIRECT_URI` 是否与 Account client 登记值逐字一致 |

验证完成后，至少检查 `/api/session`、Account 登录后 cookie 属性、WS 身份绑定、
同名 `sub` 二次登录后的 handle 稳定性和浏览器 Storage。URL、响应与 Storage
中都不应出现 Account access/refresh token。
