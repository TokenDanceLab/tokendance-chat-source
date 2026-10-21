# TokenDanceChat

> 轻量公共聊天室 · TokenDance Account OIDC · TokenBot
> A lightweight public chat product with TokenDance Account identity and an IM-native assistant

[![Status](https://img.shields.io/badge/status-active-brightgreen)](#)
[![License](https://img.shields.io/badge/license-AGPL--3.0--only-blue)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26.10-00ADD8?logo=go)](https://go.dev/)
[![React](https://img.shields.io/badge/React-19.2-61DAFB?logo=react)](https://react.dev/)
[![Vite](https://img.shields.io/badge/Vite-6.4-646CFF?logo=vite)](https://vite.dev/)

**Live**: [chat.tokendancelab.com](https://chat.tokendancelab.com)

**Container**: `ghcr.io/tokendancelab/tokendance-chat` (`latest`, `YYYYMMDD`, short SHA, and `vX.Y.Z`/`X.Y` tags from GitHub Actions)

---

## Public Packaging

TokenDanceChat is a lightweight public chat product. Its current public product contract is intentionally narrow: one public room plus a single `TokenBot` assistant, which can be chatted with either by `@TokenBot` mention in the public room or in a private 1:1 assistant workspace whose messages never leak into the public room. AgentHub is historical context only and is not a current design, architecture, or implementation reference. Public entry files live in `frontend/public/robots.txt`, `frontend/public/sitemap.xml`, and `frontend/public/llms.txt`; update them with README/site metadata when routes, product wording, PWA behavior, or TokenDance Account login semantics change.

Product/PWA assets live in `frontend/public/` as `tokendance-*` files; the Account sign-in mark lives in `frontend/public/brand/`, with provenance in its `manifest.json`. PWA, favicon, auth modal, and crawler surfaces should reference the appropriate in-repo assets. TokenDance shares typography, Logo rules, and base components across products; neutral surfaces retain each product’s existing brand accent. See [DESIGN.md](./DESIGN.md) for the asset and usage contract. `frontend/scripts/generate-icons.mjs` only keeps legacy `icon-192.png` and `icon-512.png` in sync for old browser/PWA caches; new code should not reference those names.

The frontend uses shadcn/ui with the Radix Rhea preset and the AI Elements Chat primitives (`Message Scroller`, `Message`, `Bubble`, `Marker`, `Suggestion`, `Input Group`). Reuse `frontend/src/components/ui`, `frontend/src/components/ai-elements`, and the semantic CSS variables in `frontend/src/index.css`; do not add a parallel legacy token layer or bespoke animation keyframes.

---

## 项目定位 Positioning

TokenDanceChat 是 TokenDance 的轻量公共聊天产品。当前主线是 **Chat + Account**：Chat 承担公共聊天室、TokenBot 和产品内授权；Account 提供唯一生产身份入口。AgentHub 已退出当前设计与架构参考范围。

当前产品目标是保持一个真实可用的轻量公共聊天室：

- Go Hub Server + WebSocket typed events 承载公共消息、presence、reactions、编辑、线程与 AI 流式回复；
- SQLite + FTS5 支撑聊天持久化和搜索；
- React 19 + Zustand + Vite 的客户端状态模型提供轻量但完整的聊天工作台；
- TokenBot 以单 agent、@mention 和流式回复的形态嵌入 IM；
- TokenDance Account OIDC 作为唯一生产身份入口；
- 公共聊天室、TokenBot 和安全边界作为独立产品持续演进。

跨项目的用户、内部运营成员、IAM 与邮箱边界见 [Account 平台设计](../docs/identity/account-platform-design.md)；Bot 当前行为和下一阶段方向见 [docs/bot-architecture.md](./docs/bot-architecture.md)。

---

## 功能 Features

> 完整能力清单与各能力状态（Core / Experiment / Compat / Archived）以 [docs/capability-matrix.md](./docs/capability-matrix.md) 为唯一事实来源，以下仅列当前主界面在用的核心能力。文件/图片等上传能力已退休（后端 `/api/upload` 等路由已删除，返回 404），composer 不再提供附件/图片入口。

### 核心聊天 Core Chat
- 公共聊天室
- Markdown 消息渲染（代码高亮、表格、GFM）
- 消息编辑 · 删除 · 引用回复
- 表情反应 (Emoji Reactions)
- 消息搜索（全文检索，FTS5）
- 无限滚动历史加载
- 输入状态指示（类似 Telegram typing preview）

### AI Bots & Agent
- **TokenBot** — 单 agent；公共房间 `@TokenBot` / 回复 Bot 消息，或私人助手 1:1 直接触发；浏览器通过 WebSocket 接收流式回复
- 模型由服务端 `CHAT_LLM_MODEL` 配置；前端通过 `GET /api/config` 展示真实模型名，前端不假设模型列表
- `openai-go` 调用 OpenAI-compatible Chat Completions；支持暴露该协议的模型网关
- SQLite transcript + FTS 作为 Bot 近期记忆和同会话检索；公共与私聊隔离
- 排队与回复中状态、停止本人请求、同账号多标签页同步；每个请求最多保存一条最终回复
- 公共消息展示文字与安全来源列表，原始推理、搜索词、工具参数/结果和网页全文不进入房间
- 配置启用后可搜索公开网页并读取选中来源；来源列表区分“已读取”“仅发现 · 未读取”和“读取失败”，只有本次请求成功读取并引用的来源显示引用标记

每次显式触发都固定回答该条消息，入队后的新聊天不会改写其上下文。同一房间或同一账号的私聊依次执行请求，多人请求轮流获得回复；队列满或每日预算不足时会明确提示原因。本人可在已连接的任一有效账号标签页停止排队或回复中的请求；停止确认后移除未完成草稿，已经保存的回复仍留在历史。

服务使用单实例 SQLite 保存请求状态、来源和预算。断线重连会恢复可见请求的近期状态和历史中的最终回复，不重放未完成片段；服务重启会结束尚未完成的请求，不自动重跑。运行中追加指令（steer）、自适应接话、一轮发布多条消息、多 Bot、Python 工具和后台定时/事件任务尚未实现。2026-10-21 的 Search/Fetch 为待发布增量，发布情况见 [ROADMAP.md](./ROADMAP.md) 与 [RELEASE.md](./RELEASE.md)。

网页搜索用于发现链接与摘要，读取用于取得回答依据；搜索命中不代表已读过网页。Chat 使用独立 DeepSeek Search/Fetch API，网页内容由 provider 获取，保留 `deepseek-v4.1-flash` 与 openai-go v1.12.0 的 Chat Completions 路径。来源列表和引用由服务端注册记录生成，普通 Markdown 链接仍可点击，但不会因此成为已核验来源。时间与同会话聊天检索继续可用。

### UI/UX
- shadcn/ui + AI Elements 消息 timeline、Bubble 与 Prompt Input
- Sheet / Dialog / Command 浮层与统一动画
- 暗色/亮色/跟随系统主题
- 中英文双语 (i18n)
- PWA 离线支持（Service Worker）
- 桌面通知（@mention + 新消息）
- 移动端适配（触屏手势、键盘处理）
- 无障碍 (a11y)：屏幕阅读器、键盘导航

### 安全 Security
- 本地开发密码路径：bcrypt (cost 12)；OIDC 开启时本地注册、登录与邀请路由返回 404
- TokenDance Account OIDC 登录：confidential BFF、Authorization Code + PKCE S256、ID Token RS256 校验、UserInfo 资料同步，浏览器只持有 Chat 的 HttpOnly session cookie
- 本地 login/register 频率限制为 5 次/分钟/IP；OIDC 入口也有独立限流
- CORS origin-aware（非通配符）+ WebSocket Origin 验证
- SQL 参数化查询 · FTS5 注入防护 · XSS 过滤
- 引用/线程读取和写入检查真实会话归属；未知私聊收件人拒绝发送，不能降级为公开消息
- CSP 头双层覆盖（前端 meta 标签 + 后端中间件）
- 同账号已认证标签页可同时连接；私聊输入、流式回复和请求状态仅向该账号有效会话同步。匿名同名游客仍采用旧连接接管行为
- 停止权限和用户预算按服务端稳定主体判断；Account 撤销会话后终止旧世代请求并拦截后续输出
- 路径穿越防护 · 消息大小限制
- 用户屏蔽

---

## 快速开始 Quick Start

```bash
# 1. 后端 Backend
cd backend && CHAT_ADDR=127.0.0.1:8080 go run .
# → http://localhost:8080

# 2. 另一个终端从仓库根目录启动前端 Frontend
cd frontend && pnpm install && pnpm run dev --host 127.0.0.1
# → http://localhost:5173
# VITE_PUBLIC_ORIGIN 控制 canonical/OG 的公开源（默认生产域名）
# VITE_DEV_API_TARGET 控制 /api 与 /ws 的开发代理目标（默认 127.0.0.1:8080）

# 3. 从仓库根目录构建 Single-binary build（目标为 arm64；x86 目标改 GOARCH=amd64）
cd backend
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o tokendancechat .
```

---

## 技术栈 Stack

| 层 Layer | 技术 Tech |
|----------|----------|
| 前端 Frontend | React 19 · Vite 6 · Tailwind CSS 4 · shadcn/ui · AI Elements · Zustand 5 |
| 后端 Backend | Go 1.26.10 · gorilla/websocket · net/http |
| 数据库 Database | SQLite (modernc.org/sqlite, pure Go) + FTS5 |
| 实时协议 Realtime | WebSocket typed events |
| LLM | openai-go · OpenAI-compatible Chat Completions |
| 部署 Deploy | Docker · Nginx |

这些选择构成 TokenDanceChat 自己的产品基线：React 19、Go、SQLite/FTS5、REST JSON API、WebSocket typed events，以及 IM/Agent 交互模型。

---

## WebSocket 协议 Protocol

当前前端使用公共房间与 TokenBot 私聊合同；以下是公共消息事件示例。已登录身份由 session 绑定，`join.username` 不能覆盖它：

```
→ {"type":"join","username":"alice"}
← {"type":"history","messages":[...]}
← {"type":"user_joined","username":"alice","online":[...]}

→ {"type":"message","content":"hello"}
← {"type":"message","id":"...","username":"alice","content":"hello","timestamp":...}

→ {"type":"reaction","message_id":"...","emoji":"👍"}
→ {"type":"message_edit","message_id":"...","content":"edited text"}
→ {"type":"block","username":"spammer"}
```

TokenBot 的一次回复由 `run_id` 标识。客户端通过 `GET /api/config` 的嵌套字段 `capabilities.run_control` 判断是否显示停止入口；该值只有在 LLM 与持久 run 存储可用时才为 `true`，不存在顶层 `run_control` 字段：

```json
{"capabilities":{"run_control":true}}
```

```text
← {"type":"bot_run","run_id":"r1","message_id":"m1","stream_id":"r1","status":"queued","trigger_message_id":"q1","requested_by":"alice","room_id":"room1","timestamp":1792576800000}
→ {"type":"bot_cancel","run_id":"r1"}
```

状态为 `queued | running | completed | failed | cancelled`。`requested_by` 用于展示发起人，停止权限由服务端会话校验；无权停止与不存在的请求均返回 `RUN_UNAVAILABLE`。队列满和预算不足分别返回 `RUN_QUEUE_FULL` 与 `BOT_BUDGET_EXCEEDED`。断线时不缓存停止指令，重新连接后应以补发的状态为准。私聊事件带 `private:true`，公共事件可以省略该字段；私聊内容与状态只送达同账号有效会话。完整行为和隔离合同见 [Bot 架构](./docs/bot-architecture.md)。

网页工具使用 `capabilities.web_search` 表示实际配置可用。最终消息和历史中的 `source` part 仅含 `source_id`、`title`、`url`、`status`（`discovered | fetched | failed`）与 `cited`；来源绑定当前 run，只有成功读取的来源可被引用。内部来源标记不会出现在流式文字中，服务端在最终提交时将有效引用转换为编号链接。

历史 IM 协议（DM、群组、好友、转发、通话、定时消息、webhook 管理、普通文件上传）已从后端运行面删除并退役；当前前端主界面只有公共聊天室与 TokenBot 私聊/mention 两种会话形态。若要恢复任何历史能力，先更新 ROADMAP 中的轻量聊天合同并补足视觉/E2E 证据。

---

## 配置 Config

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `CHAT_ADDR` | `:8080` | 监听地址 |
| `CHAT_DB_PATH` | `data/chat.db` | SQLite 路径 |
| `CHAT_FRONTEND_DIR` | `frontend/dist` | 前端静态文件 |
| `CHAT_LLM_BASE_URL` | — | LLM API 地址（如 `https://api.deepseek.com`） |
| `CHAT_LLM_MODEL` | — | 模型名（如 `deepseek-v4.1-flash`）；由服务端配置，前端经 `/api/config` 展示 |
| `CHAT_LLM_API_KEY` | — | API 密钥 |
| `CHAT_LLM_PROVIDER` | —（未设置时禁用 Bot） | 运维标签（openai / deepseek / gateway…），协议固定为 OpenAI Chat Completions；设置后必须同时提供 `CHAT_LLM_MODEL` 与 `CHAT_LLM_API_KEY`，否则启动失败 |
| `CHAT_LLM_MAX_TOKENS` | `4096` | 每轮模型输出最大 token；完整工具循环另受 run 预算约束 |
| `CHAT_LLM_TEMPERATURE` | — | 采样温度，留空用服务端默认 |
| `CHAT_LLM_TOP_P` | — | nucleus sampling，留空用服务端默认 |
| `CHAT_LLM_TIMEOUT` | `90s` | 单次 HTTP 请求超时（Go duration）；run 执行还受 60 秒上限、入队起 120 秒总期限约束 |
| `CHAT_LLM_MAX_TOOL_ROUNDS` | `3` | 工具调用最大轮数 |
| `CHAT_LLM_REASONING_EFFORT` | — | `low` / `medium` / `high`；模型不支持时留空 |
| `CHAT_LLM_MEMORY_SIZE` | `20` | 上下文窗口消息条数；记忆直接来自 SQLite 会话记录，不另存文件 |
| `CHAT_BOT_NAME` | `TokenBot` | Bot 名称 |
| `CHAT_BOT_DAILY_PER_USER` | `50` | 每个稳定用户主体每天接纳的请求数；按 HKT 日累计，`0` 关闭此项限制 |
| `CHAT_BOT_DAILY_PER_IP` | `200` | 每个 IP 每天接纳的请求数；IPv6 按 /64 聚合，`0` 关闭此项限制 |
| `CHAT_BOT_DAILY_REPLIES` | `800` | 全实例每天接纳的请求数，`0` 关闭此项限制 |
| `CHAT_BOT_DAILY_TOKENS` | `3000000` | 全实例每日 token 预算；包含在途请求的保守预留，`0` 关闭此项限制 |
| `CHAT_BOT_MAX_CONCURRENT` | `4` | 全实例同时执行上限，硬上限 16；`0` 回退默认 4，同一会话仍只执行一个请求 |
| `CHAT_WEB_SEARCH_ENABLED` | `false` | 显式启用 DeepSeek 独立 Search/Fetch；需同时启用 LLM 并提供独立工具凭据 |
| `CHAT_WEB_SEARCH_BASE_URL` | `https://api.deepseek.com/v1` | 工具 API base，必须为 HTTPS 且路径以 `/v1` 结尾；使用网关时须确认两个工具路由均可用 |
| `CHAT_WEB_SEARCH_API_KEY` | — | 启用时必填；不会自动复用或转发 `CHAT_LLM_API_KEY` |
| `CHAT_WEB_SEARCH_DAILY_MICRO_CNY` | `5000000` | 全实例每日应用工具预算，单位为百万分之一元；默认 ¥5，允许 5000–1000000000，不支持以 `0` 取消限制 |
| `CHAT_ALLOWED_ORIGINS` | — | 允许的跨源 CORS/WebSocket browser origins，必须包含 scheme；示例：`https://chat.example.com,https://*.example.com`，`*` 不会放行跨源请求 |
| `CHAT_TRUSTED_PROXY_CIDRS` | — | 可信反代 IP/CIDR；只有这些来源的 `X-Forwarded-For` / `X-Real-IP` 会用于 REST、OIDC、auth、WS 限流 |
| `CHAT_SESSION_SECRET` | — | `tdchat_session` cookie 的 HMAC 签名 secret；OIDC 开启时缺失会在打开数据库前拒绝启动；`docker compose` 部署会强制要求该值 |
| `CHAT_COOKIE_SECURE` | `true` | 是否给 session cookie 加 `Secure`；生产保持 `true`，仅 issuer 与 callback 都是 loopback 时可显式设为 `false` |
| `CHAT_OIDC_ENABLED` | `false` | 是否启用 OIDC 登录 |
| `CHAT_OIDC_ISSUER` | — | TokenDance Account issuer，生产为 `https://account.tokendancelab.com` |
| `CHAT_OIDC_CLIENT_ID` | — | OAuth client id；启用 OIDC 时必须由部署环境提供 |
| `CHAT_OIDC_CLIENT_SECRET` | — | Confidential client 密钥；启用 OIDC 时必填，只由后端用于 HTTP Basic token exchange |
| `CHAT_OIDC_REDIRECT_URI` | `http://localhost:8080/api/oidc/callback` | OIDC 回调地址；生产使用部署域名的 `/api/oidc/callback` |
| `CHAT_OIDC_POST_LOGOUT_REDIRECT_URI` | 从 callback origin 派生 `/api/logout/account/callback` | Account 全局退出后返回 Chat 的精确后端回调；生产应显式设为 `https://chat.tokendancelab.com/api/logout/account/callback`，并与 Account client 登记值逐字一致 |

队列最多保留 64 个待执行请求，每个会话最多 8 个、每个用户主体跨会话最多 2 个。每日预算在入队时记账并预留完整工具循环的保守 token 上限，因此短回复也可能先占用较大额度。仅当所有模型轮次都返回完整用量时按实际 token 结算；用量缺失或执行中停止保留预留，尚未执行便停止则释放 token 预留，所有已接纳请求仍计次数。记账日固定为入队时的 HKT 日期，跨日与进程重启不会重置已有记录；该账本用于限制本实例消耗，不替代模型提供方账单。

网页工具另设每 run 最多 3 次的调用上限，每次发起前按版本费率预留 ¥0.005，默认每日 ¥5。明确的 4xx 拒绝（408 除外）和 HTTP 200 空结果可结算为零；超时、取消、5xx 或无法判断的结果保留预留，重启后仍保留记录。费率依据 [DeepSeek 2026-10-17 公告](https://api-docs.deepseek.com/zh-cn/news/news261017/)的 Search/Fetch 各 ¥5/千次；这是应用消耗预算，不自动换汇，也不保证等于网关最终账单。接口、provider 抓取边界和 SDK 区别见 [Bot 架构](./docs/bot-architecture.md)。部署时应显式配置工具 base/key 与启用开关，并先验证目标 endpoint。

---

## OIDC Relying-Party Checklist

TokenDanceChat 是服务端型 OIDC consumer；详细步骤见 [docs/oidc-setup.md](./docs/oidc-setup.md)，身份契约以 TokenDance Account 的 [`docs/oidc-integration.md`](../tokendance-account/docs/oidc-integration.md) 和公开 discovery 为准。

| 项 | 当前实现 |
|----|----------|
| Callback | 本地 `http://localhost:8080/api/oidc/callback`；生产 `https://chat.tokendancelab.com/api/oidc/callback`。本地和生产使用独立 client |
| Token exchange | 后端用 `client_secret_basic` 兑换 authorization code；PKCE verifier 仍随请求发送。access token、ID token 和 refresh token不进入浏览器 |
| Browser session | Account 验证成功后，Chat 签发 `tdchat_session`：`HttpOnly`、`SameSite=Lax`、默认 `Secure`、7 天有效 |
| Identity | `sub` 是 Account 用户的唯一稳定键，也是 run 归属与用户预算的依据；首次登录选择可读 Chat handle，之后不再漂移。新消息记录服务端主体，旧历史不猜测补写身份 |
| Profile | 后端读取 UserInfo，校验其 `sub` 与 ID Token 一致，并把 `name`、`picture` 同步到 Chat profile；`preferred_username` 视为可选 |
| Provider bounds | discovery、JWKS、token 和 UserInfo 请求使用 5s timeout 与响应体上限；state 存储有容量上限，未知 `kid` 会强制刷新一次 JWKS |
| Session boundary | REST 与 WebSocket upgrade 都从同源 `tdchat_session` cookie 绑定身份；OIDC session 绑定 Account `sub` 和持久化 logout generation；WS `join` 不携带 token，并会在注册前后重验 generation |
| Logout | `离开聊天` 只清 Chat；`退出 TokenDance Account` 先清本地 session，再跳转 Account end-session。Account back-channel logout 按 `sub` 推进 generation，同时终止旧世代 run 并断开对应 live WS |

## 目录结构 Structure

```
TokenDanceChat/
├── backend/           # Go 后端
│   ├── handler/       # HTTP + WS handler
│   │   └── oidc.go    # TokenDance Account confidential OIDC BFF
│   ├── hub/           # 聊天核心（client、room、broadcast）
│   ├── store/         # SQLite + FTS5
│   ├── llm/           # OpenAI-compatible Chat Completions + 工具循环
│   └── main.go
├── frontend/          # React SPA
│   ├── src/
│   │   ├── components/   # UI 组件
│   │   │   ├── AuthModal.tsx
│   │   │   └── OidcLoginButton.tsx
│   │   ├── hooks/        # 自定义 Hooks
│   │   ├── stores/       # Zustand 状态
│   │   ├── i18n/         # 国际化
│   │   ├── lib/          # 工具 + API 客户端 + Registry
│   │   └── main.tsx
│   └── public/        # 静态资源 + Service Worker
├── docs/              # 文档
├── scripts/           # 构建/部署脚本
├── nginx/             # Nginx 配置
├── Dockerfile.runtime # 运行时镜像
├── docker-compose.yml
└── ROADMAP.md
```

---

## 部署 Deploy

使用 Docker Compose（`docker-compose.yml`）：

```bash
cp .env.example .env        # 按需填写 CHAT_SESSION_SECRET / CHAT_LLM_* / OIDC
docker compose up -d --build
# → http://localhost:8080
```

项目级 Agent 接手规则见 [AGENTS.md](./AGENTS.md)，发布检查清单见 [RELEASE.md](./RELEASE.md)，持续目标和当前增量见 [ROADMAP.md](./ROADMAP.md)。生产部署须在 `.env` 中提供 `CHAT_SESSION_SECRET`，并显式配置 `CHAT_LLM_*` 以启用 TokenBot。

---

## TokenDance 系统上下文

在多仓 workspace 内做跨系统治理时，先看 workspace 根级 `AGENTS.md` 和 `docs/`。当前主线为 Chat + Account；AgentHub 不作为当前设计、架构或实现参考。身份契约以 [TokenDance Account 开发者文档](https://account.tokendancelab.com/developers) 和 `tokendance-account/docs/oidc-integration.md` 为准，视觉方向以 `tokendance-design/docs/design/design-system.md` 为准。

---

## 文档索引 Docs

| 文档 | 内容 |
|------|------|
| `../docs/` | TokenDance 系统级架构、身份鉴权、设计系统、文档治理 |
| [docs/bot-architecture.md](./docs/bot-architecture.md) | TokenBot 当前行为、隔离边界和后续实施方向 |
| [Account 平台设计](../docs/identity/account-platform-design.md) | 跨项目用户、运营成员、IAM 与邮箱方案（拟实施） |
| [docs/engineering-goal.md](./docs/engineering-goal.md) | 长期工程目标、设计原则、验证要求 |
| [docs/webhook-integration.md](./docs/webhook-integration.md) | 历史群组传入 Webhook 协议与安全契约；当前前端主界面不暴露 |
| [docs/visual-acceptance.md](./docs/visual-acceptance.md) | 前端截图、多模态审美和视觉验收标准 |
| [ROADMAP.md](./ROADMAP.md) | 持续目标账本、当前增量、验证记录 |
| [AGENTS.md](./AGENTS.md) | 项目级 Agent 接手规则、架构地图、验证命令 |
| [RELEASE.md](./RELEASE.md) | 发布/部署检查清单 |
| [SECURITY.md](./SECURITY.md) | 安全审计报告 |
| [CHANGELOG.md](./CHANGELOG.md) | 变更日志 |
| [docs/research-deeix-lobehub.md](./docs/research-deeix-lobehub.md) | DEEIX-Chat + LobeHub Icons 调研 |
| [docs/llm-api-reference.md](./docs/llm-api-reference.md) | LLM API 参考 |

---

## License

Copyright (c) 2026 TokenDanceChat Contributors.

本项目当前版本采用 [GNU AGPL v3 only（AGPL-3.0-only）](./LICENSE)，不附加“禁止商用”或“禁止安全研究”条款。允许商业使用；分发以及通过网络提供修改版服务时，须履行许可证中适用的对应源码提供义务。程序不提供担保，详见许可证。

源码公开面、私有运行配置和对应源码发布门禁见
[docs/source-boundary.md](./docs/source-boundary.md)。当前开发仓为 private 时，
private repo 链接不构成对公网用户的源码交付，正式 Release 必须使用公开源码镜像
或公开的版本化 source archive。

本次切换不撤销旧版本的 MIT 授权：提交 `3d5a1ce958fcffdeecbac64d032887c7548cefcd` 及之前已发布的项目代码仍可按原 MIT 条款使用，原文保存在 [LICENSES/MIT-legacy.txt](./LICENSES/MIT-legacy.txt)。第三方代码、组件、字体及图标保留各自的许可证与版权声明，见 [NOTICE](./NOTICE)。新版本作为整体按 AGPL-3.0-only 提供。

Current releases are licensed under AGPL-3.0-only. Commercial use is permitted. Distribution and modified network services carry the corresponding source obligations specified by the license. Earlier MIT grants and third-party licenses remain intact. The software is provided without warranty.

运行服务时保留界面中的“源码与许可证”入口，并指向实际运行版本的完整对应源码（包括构建脚本和修改），不要仅链接到未修改的上游。官方容器构建通过 `SOURCE_REVISION` 固定源码提交；自行构建可用 `VITE_SOURCE_URL` 设置自己的源码入口。源码不包括用户聊天数据、凭据或生产密钥。

AGPL 不是访问控制或防攻击机制，也不授权对线上服务进行扫描、爆破或压测。漏洞请通过 [私密安全报告](https://github.com/TokenDanceLab/tokendance-chat-source/security/advisories/new) 提交；报告渠道见 [SECURITY.md](./SECURITY.md)。
