# TokenDanceChat

> 轻量公共聊天室 · TokenDance Account OIDC · TokenBot
> A lightweight public chat product with TokenDance Account identity and an IM-native assistant

[![Status](https://img.shields.io/badge/status-active-brightgreen)](#)
[![License](https://img.shields.io/badge/license-AGPL--3.0--only-blue)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev/)
[![React](https://img.shields.io/badge/React-19.2-61DAFB?logo=react)](https://react.dev/)
[![Vite](https://img.shields.io/badge/Vite-6.4-646CFF?logo=vite)](https://vite.dev/)

**Live**: [chat.tokendancelab.com](https://chat.tokendancelab.com)

**Container**: `ghcr.io/tokendancelab/tokendance-chat` (`latest`, `YYYYMMDD`, short SHA, and `vX.Y.Z`/`X.Y` tags from GitHub Actions)

---

## Public Packaging

TokenDanceChat is a lightweight public chat product. Its current public product contract is intentionally narrow: one public room plus a single `TokenBot` assistant, which can be chatted with either by `@TokenBot` mention in the public room or in a private 1:1 assistant workspace whose messages never leak into the public room. AgentHub mappings are optional references, not a current dependency or roadmap gate. Public entry files live in `frontend/public/robots.txt`, `frontend/public/sitemap.xml`, and `frontend/public/llms.txt`; update them with README/site metadata when routes, product wording, PWA behavior, or TokenDance Account login semantics change.

Brand assets live in `frontend/public/` as `tokendance-*` files. PWA, favicon, auth modal, and crawler surfaces should reference those in-repo assets. `frontend/scripts/generate-icons.mjs` only keeps legacy `icon-192.png` and `icon-512.png` in sync for old browser/PWA caches; new code should not reference those names.

The frontend uses shadcn/ui with the Radix Rhea preset and the AI Elements Chat primitives (`Message Scroller`, `Message`, `Bubble`, `Marker`, `Suggestion`, `Input Group`). Reuse `frontend/src/components/ui`, `frontend/src/components/ai-elements`, and the semantic CSS variables in `frontend/src/index.css`; do not add a parallel legacy token layer or bespoke animation keyframes.

---

## 项目定位 Positioning

TokenDanceChat 是 TokenDance 的轻量公共聊天产品。当前主线是 **Chat + Account**：Chat 承担公共聊天室、TokenBot 和产品内授权；Account 提供唯一生产身份入口。AgentHub 是未来的可选集成与复用参考，不是当前优先级、依赖或发布门禁。

当前产品目标是保持一个真实可用的轻量公共聊天室：

- Go Hub Server + WebSocket typed events 承载公共消息、presence、reactions、编辑、线程与 AI 流式回复；
- SQLite + FTS5 支撑聊天持久化和搜索；
- React 19 + Zustand + Vite 的客户端状态模型提供轻量但完整的聊天工作台；
- TokenBot 以单 agent、@mention 和流式回复的形态嵌入 IM；
- TokenDance Account OIDC 作为唯一生产身份入口；
- 公共聊天室、TokenBot 和安全边界作为独立产品持续演进。

可选的 AgentHub 映射参考见 [docs/agenthub-validation.md](./docs/agenthub-validation.md)。

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
- **TokenBot** — 单 agent，`@TokenBot` 触发 LLM 对话（流式 SSE）
- 模型由服务端 `CHAT_LLM_MODEL` 配置；前端通过 `GET /api/config` 展示真实模型名，前端不假设模型列表
- Anthropic Messages API + OpenAI Chat Completions 双协议
- Bot 对话记忆持久化

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
- 密码：bcrypt (cost 12) 哈希，SHA-256 旧哈希登录时自动升级
- TokenDance Account OIDC 登录：confidential BFF、Authorization Code + PKCE S256、ID Token RS256 校验、UserInfo 资料同步，浏览器只持有 Chat 的 HttpOnly session cookie
- Auth 频率限制：login/register 5 次/分钟/IP
- CORS origin-aware（非通配符）+ WebSocket Origin 验证
- SQL 参数化查询 · FTS5 注入防护 · XSS 过滤
- CSP 头双层覆盖（前端 meta 标签 + 后端中间件）
- 会话挤下线：新登录自动踢出旧连接
- 路径穿越防护 · 消息大小限制
- 用户屏蔽

---

## 快速开始 Quick Start

```bash
# 1. 后端 Backend
cd backend && go run .
# → http://localhost:8080

# 2. 前端 Frontend
cd frontend && pnpm install && pnpm run dev
# → http://localhost:5173
# VITE_PUBLIC_ORIGIN 控制 canonical/OG 的公开源（默认生产域名）
# VITE_DEV_API_TARGET 控制 /api 与 /ws 的开发代理目标（默认 127.0.0.1:8080）

# 3. 单二进制构建 Single-binary build（生产 生产主机 = arm64；x86 主机改 GOARCH=amd64）
cd backend
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o tokendancechat .
```

---

## 技术栈 Stack

| 层 Layer | 技术 Tech |
|----------|----------|
| 前端 Frontend | React 19 · Vite 6 · Tailwind CSS 4 · shadcn/ui · AI Elements · Zustand 5 |
| 后端 Backend | Go 1.25 · gorilla/websocket · net/http |
| 数据库 Database | SQLite (modernc.org/sqlite, pure Go) + FTS5 |
| 实时协议 Realtime | WebSocket typed events |
| LLM | Anthropic Messages API + OpenAI Chat Completions |
| 部署 Deploy | Docker · Nginx |

这些选择构成 TokenDanceChat 自己的产品基线：React 19、Go、SQLite/FTS5、REST JSON API、WebSocket typed events，以及 IM/Agent 交互模型。

---

## WebSocket 协议 Protocol

当前前端只依赖公共聊天室主合同：

```
→ {"type":"join","username":"alice"}
← {"type":"history","messages":[...]}
← {"type":"user_joined","username":"alice","online":[...]}

→ {"type":"message","content":"hello"}
← {"type":"message","id":"...","username":"alice","content":"hello","timestamp":...}

→ {"type":"reaction","message_id":"...","emoji":"👍"}
→ {"type":"edit","message_id":"...","content":"edited text"}
→ {"type":"block","username":"spammer"}
```

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
| `CHAT_LLM_PROVIDER` | `openai` | 运维标签（openai / deepseek / gateway…），协议固定为 OpenAI Chat Completions；设置后必须同时提供 `CHAT_LLM_MODEL` 与 `CHAT_LLM_API_KEY`，否则启动失败 |
| `CHAT_LLM_MAX_TOKENS` | `4096` | 单次回复最大 token |
| `CHAT_LLM_TEMPERATURE` | — | 采样温度，留空用服务端默认 |
| `CHAT_LLM_TOP_P` | — | nucleus sampling，留空用服务端默认 |
| `CHAT_LLM_TIMEOUT` | `90s` | 单次请求超时（Go duration，如 `90s`、`2m`） |
| `CHAT_LLM_MAX_TOOL_ROUNDS` | `3` | 工具调用最大轮数 |
| `CHAT_LLM_REASONING_EFFORT` | — | `low` / `medium` / `high`；模型不支持时留空 |
| `CHAT_LLM_MEMORY_SIZE` | `20` | 上下文窗口消息条数；记忆直接来自 SQLite 会话记录，不另存文件 |
| `CHAT_BOT_NAME` | `TokenBot` | Bot 名称 |
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

---

## OIDC Relying-Party Checklist

TokenDanceChat 是服务端型 OIDC consumer；详细步骤见 [docs/oidc-setup.md](./docs/oidc-setup.md)，身份契约以 TokenDance Account 的 [`docs/oidc-integration.md`](../tokendance-account/docs/oidc-integration.md) 和公开 discovery 为准。

| 项 | 当前实现 |
|----|----------|
| Callback | 本地 `http://localhost:8080/api/oidc/callback`；生产 `https://chat.tokendancelab.com/api/oidc/callback`。本地和生产使用独立 client |
| Token exchange | 后端用 `client_secret_basic` 兑换 authorization code；PKCE verifier 仍随请求发送。access token、ID token 和 refresh token不进入浏览器 |
| Browser session | Account 验证成功后，Chat 签发 `tdchat_session`：`HttpOnly`、`SameSite=Lax`、默认 `Secure`、7 天有效 |
| Identity | `sub` 是 Account 用户的唯一稳定键；首次登录从 `preferred_username`、邮箱 local-part 或 `name` 选择可读 Chat handle，之后不再漂移 |
| Profile | 后端读取 UserInfo，校验其 `sub` 与 ID Token 一致，并把 `name`、`picture` 同步到 Chat profile；`preferred_username` 视为可选 |
| Provider bounds | discovery、JWKS、token 和 UserInfo 请求使用 5s timeout 与响应体上限；state 存储有容量上限，未知 `kid` 会强制刷新一次 JWKS |
| Session boundary | REST 与 WebSocket upgrade 都从同源 `tdchat_session` cookie 绑定身份；OIDC session 绑定 Account `sub` 和持久化 logout generation；WS `join` 不携带 token，并会在注册前后重验 generation |
| Logout | `离开聊天` 只清 Chat；`退出 TokenDance Account` 先清本地 session，再跳转 Account end-session。Account back-channel logout 会按 `sub` 使 generation 前进并断开对应 live WS |

## 目录结构 Structure

```
TokenDanceChat/
├── backend/           # Go 后端
│   ├── handler/       # HTTP + WS handler
│   │   └── oidc.go    # TokenDance Account confidential OIDC BFF
│   ├── hub/           # 聊天核心（client、room、broadcast）
│   ├── store/         # SQLite + FTS5
│   ├── llm/           # LLM adapter（Anthropic + OpenAI）
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

在多仓 workspace 内做跨系统治理时，先看 workspace 根级 `AGENTS.md` 和 `docs/`。TokenDanceChat 与 AgentHub 都是独立产品；当前优先完成 Chat + Account。身份契约以 [TokenDance Account 开发者文档](https://account.tokendancelab.com/developers) 和 `tokendance-account/docs/oidc-integration.md` 为准，视觉方向以 `tokendance-design/docs/design/design-system.md` 为准。

---

## 文档索引 Docs

| 文档 | 内容 |
|------|------|
| `../docs/` | TokenDance 系统级架构、身份鉴权、设计系统、文档治理 |
| [docs/agenthub-validation.md](./docs/agenthub-validation.md) | 可选 AgentHub 集成、历史验证与技术栈映射 |
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
