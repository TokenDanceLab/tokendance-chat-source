# TokenDance Chat 安全风险登记

最后复核：2026-10-21　范围：公共聊天室 + TokenBot + Account OIDC BFF + 表情上传 +
链接预览。群组、通话、webhook 已从运行面移除，相关旧条目不再适用。

## 资产

- 账号与会话：本地密码（bcrypt cost 12）、HMAC 会话 token、OIDC 链接。
- 消息：公共房间与「用户 ↔ TokenBot」私聊；私聊互不可见。
- 模型凭据与额度：`CHAT_LLM_API_KEY`、每用户冷却与工具轮次上限。
- 可用性：HTTP/WebSocket 接入、SQLite 写入、上传目录。

## 当前控制

| ID | 等级 | 状态 | 风险 | 控制与证据 |
|---|---|---|---|---|
| SEC-01 | 高 | 已实现（仅本地 OIDC-disabled 面） | 撞库/爆破 | 本地密码面使用 Turnstile + 每账号/每 IP 退避；生产 OIDC 模式不注册本地登录路由；`security/login_guard_test.go`、`handler_test.go TestLoginBackoffAppliesAcrossAddresses` |
| SEC-02 | 高 | 已实现 | 注册机器人 | 邀请码 + Turnstile；`handler.go Register`、`security/turnstile_test.go` |
| SEC-03 | 高 | 已实现 | 代理头伪造导致全站共享限流桶 | 可信 CIDR + `CF-Connecting-IP` 优先；`security/proxy_test.go`、`handler/ratelimit_test.go` |
| SEC-04 | 高 | 已实现 | HTTP/WS 洪泛 | 边缘 CF → nginx `limit_req/limit_conn` → Go 窗口限流；`docs/security-architecture.md` |
| SEC-05 | 高 | 已实现 | 会话伪造 | `tdchat_session` 使用 HMAC-SHA256、常量时间比较、7 天过期，并设置 `HttpOnly`、默认 `Secure`、`SameSite=Lax`；`handler.go issueSessionToken/verifySessionToken` |
| SEC-06 | 中 | 已实现 | 账号枚举 | 登录统一错误文案 + 未知用户 dummy bcrypt；`store.go VerifyUser` |
| SEC-07 | 中 | 已实现 | 请求体耗尽内存 | `BodyLimitMiddleware`：JSON 16 KiB，上传 128 KiB；`handler/ratelimit_test.go` |
| SEC-08 | 中 | 已实现 | SSRF（链接预览） | HTTPS-only、私网/环回/链路本地拒绝、拨号前二次校验；`handler.go` link preview |
| SEC-09 | 中 | 已实现 | 搜索结果越权 | 公共搜索强制 `to_user=''`；私聊搜索锁定本人线程；`hub/tools_test.go` |
| SEC-10 | 中 | 已实现 | LLM 成本滥用 | 3 秒冷却、单飞、4096 输出上限、3 轮工具、8000 字符工具结果 |
| SEC-11 | 中 | 已实现 | Account token 暴露面 | 浏览器和 URL 不接收 Account access/refresh token；OIDC token 只存在于后端回调期间；API 响应不进 Service Worker 缓存 |
| SEC-12 | 低 | 已实现 | 安全头/点击劫持 | CSP、`X-Frame-Options`、`nosniff`、Referrer/Permissions Policy |
| SEC-13 | 中 | 待决策 | 邀请码可被任意登录用户生成 | 需要产品角色策略（管理员 / 邀请配额）；当前由登录态 + 认证限流兜底 |
| SEC-14 | 中 | 已实现（委派） | 账号第二因子 | OIDC 模式由 TokenDance Account 提供 Passkey/TOTP；Chat 不复制账户安全状态 |
| SEC-15 | 低 | 待决策 | 限流为单进程 | 多副本部署前必须迁移到 Redis 或全部下沉 Cloudflare |
| SEC-16 | 高 | 已实现 | OIDC 模式下残留本地密码入口 | OIDC 启用时 `/api/register`、`/api/login` 和邀请处理器返回 404；只保留 Account 与游客入口 |
| SEC-17 | 高 | 已实现 | Logout Token 重放、伪造或同 JTI 替换 | RS256/JWKS、issuer/audience/time/event/sub 严格校验；8 KiB form 边界；SQLite JTI + claims hash；同 claims 幂等、不同 claims 拒绝；`handler/oidc_logout_test.go` |
| SEC-18 | 高 | 已实现 | Account 已退出但 Chat REST/WS 仍有效 | `sub` + 持久化 generation 进入 OIDC cookie；REST、WS upgrade、join、Hub register 四次核对；live WS typed event 先刷出再关闭；旧 Cookie 与 pre-join race 均有测试 |
| SEC-19 | 中 | 已实现 | 伪造未知 `kid` 放大 JWKS 出站请求 | 首次未知 kid 刷新支持轮转，随即进入 30 秒强制刷新冷却；`handler/oidc_test.go` |

## 部署验证

```bash
# 本机门禁
cd backend && go build ./... && go vet ./... && go test ./...
cd frontend && pnpm exec tsc --noEmit && pnpm test && pnpm run build

# 生产（只读/无副作用）
curl -fsS https://chat.tokendancelab.com/api/health
# 生产 OIDC 模式的旧本地登录面必须不存在；不要向生产伪造登录请求。
curl -s -o /dev/null -w '%{http_code}\n' -X POST https://chat.tokendancelab.com/api/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"__oidc_route_probe__","password":"x"}'
```

预期：健康检查 200；生产 OIDC 模式的本地登录路由返回 404，而不是重新出现
第二套密码身份面。Turnstile 的 403/503 负向校验只在隔离的
`CHAT_OIDC_ENABLED=false` 测试配置中执行，不使用生产账号或生产入口。

## 复核节奏

- 每次发布：跑完上述门禁并检查 429/403 日志。
- 每季度：复核 Cloudflare WAF/速率规则、Turnstile hostname/action、可信代理网段。
- 变更 Docker 网络或反代拓扑后：立即核对 `CHAT_TRUSTED_PROXY_CIDRS`。
