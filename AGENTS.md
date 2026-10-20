# TokenDanceChat — 项目 Agent 交接 SSOT

> 本文件是项目级 Agent 交接的唯一事实来源（SSOT）：接手 TokenDanceChat 的 agent 先读本文件。
> TokenBot 的 system prompt 由 `backend/hub/prompt.go` 的 `BuildPrompt` 生成（版本 `TokenBotPromptVersion`），按模式静态、可缓存；对话记忆直接取自 SQLite（`backend/hub/history.go`）。本文件不参与 bot 上下文。

最后更新：2026-10-21 HKT

## 项目定位

TokenDanceChat 是 TokenDance 的轻量公共聊天产品。当前主线是 **Chat + Account**：

- Chat 负责公共聊天室、TokenBot、实时协议和产品内授权；
- TokenDance Account 是唯一生产身份入口；
- [AgentHub](https://github.com/TokenDanceLab/AgentHub) 是未来的可选集成与复用参考，不是当前依赖、发布门禁或路线优先级。

代码采用 AGPL-3.0-only；当前开发仓为 private，部署必须另行提供公开、可下载且与运行版本一致的 Corresponding Source。当前产品目标：

- Go Hub Server + WebSocket typed events 承载公共消息、presence、reactions、编辑、线程与 AI 流式回复；
- SQLite（modernc.org/sqlite，pure Go）+ FTS5 支撑早期 Hub 持久化与全文搜索；
- React 19 + Zustand + Vite 的客户端状态模型跑通轻量聊天工作台；
- 公共协议 mention 能否让 AI 以 IM 参与者的形态自然嵌入；
- TokenDance Account OIDC 作为统一身份入口接入 IM；Chat 只做 confidential BFF，不维护第二套生产账号。

能力状态与退役清单以 `docs/capability-matrix.md` 为唯一事实来源；
`docs/agenthub-validation.md` 只保留可选的 AgentHub 映射参考。

## 技术栈

| 层 | 技术 |
|---|---|
| 前端 | React 19 · Vite 6 · Tailwind CSS 4 · shadcn/ui (Radix Rhea) · AI Elements · Zustand 5 |
| 后端 | Go 1.25 · gorilla/websocket · net/http |
| 数据库 | SQLite（pure Go）+ FTS5 |
| 实时协议 | WebSocket typed events |
| LLM | `github.com/openai/openai-go`（OpenAI-compatible Chat Completions；DeepSeek / Gateway） |
| 部署 | Docker · Nginx |

## 目录地图

```
backend/            # Go 后端
  handler/          # HTTP + WS handler + Account OIDC BFF + GET /api/config
  hub/              # 聊天核心（client / room / broadcast）
  store/            # SQLite + FTS5
  llm/              # LLM adapter（Anthropic + OpenAI）
  main.go           # 入口：配置读取、路由注册、writeAgentsMD
frontend/
  src/components/   # UI 组件（MessageTranscript / MessageBubble / ChatInput / Sidebar…）
  src/stores/       # Zustand 状态
  src/hooks/        # 自定义 Hooks
  src/lib/          # 工具 + API 客户端 + Registry
  src/i18n/         # 国际化
  public/           # 静态资源 + PWA（robots.txt / sitemap.xml / llms.txt）
docs/               # 文档（验证边界 / 能力矩阵 / 视觉验收 / 安全）
scripts/            # 构建/部署脚本
nginx/              # Nginx 配置
```

## 当前产品合同

收敛为**公共聊天室 + TokenBot 单 agent（双会话形态）**：

- 公共聊天室：消息渲染、编辑/删除/引用回复、reaction、搜索、线程、无限滚动、输入状态；`@TokenBot` mention 触发 bot 在房间内公开流式回复。
- 私人助手 1:1：侧边栏选 TokenBot 进入独立私聊视图；消息 `to=BotName` 走私聊通道（后端不广播、只回发起者，`Message.Private`），历史经 `GET /api/messages?to=`（鉴权、按请求者作用域）加载。公共与私聊消息流互不可见。
- TokenBot：单 bot，LLM 流式回复；模型由服务端 `CHAT_LLM_MODEL` 配置，前端通过 `GET /api/config` 展示真实模型名，前端不假设模型列表。
- Bot 的触发、记忆边界、prompt 缓存与工具暴露以 `docs/bot-architecture.md` 为准；改 prompt 或工具签名要同步 bump `TokenBotPromptVersion`。
- 其它历史 IM 能力（DM、群组、语音/视频、GIF、定时发送、转发、webhook 管理、普通文件上传、文件夹）已于 2026-08-31 从后端运行面删除（Archived）。若要恢复，必须先更新 `ROADMAP.md` 并补足视觉/E2E 证据。

前端 UI 只用 `frontend/src/components/ui` 与 `frontend/src/components/ai-elements` 的现成组件和语义 token，不新增并行设计系统、旧 `--td-*` 别名或自定义动画 keyframes。

## 验证命令

```bash
# 后端
cd backend && go build ./... && go test ./... && go vet ./...

# 前端
cd frontend && pnpm exec tsc --noEmit && pnpm test && pnpm run build
```

提交前再跑 `git diff --check`。每次有意义的前端打磨须用真实浏览器截图验收（视觉验收流程见 `docs/visual-acceptance.md`）。

## 已知坑（改前必读）

- **arm64 宿主禁止 `platform: linux/amd64`**：qemu/binfmt 模拟层已退役，写死 amd64 必然 `exec format error`（曾导致容器崩溃循环 + 公网 502）。构建或拉取 arm64 产物，部署前断言架构（`file` 对比 `uname -m`）。
- **Service Worker 缓存**：返回用户会跑旧包——「UI 没生效」先查 `CACHE_NAME` 是否 bump；导航请求必须 network-first。
- **`git commit -m` 含反引号**会触发 shell 命令替换、截断提交信息；提交信息用单引号或写进文件（`-F`）。
- **`go run .` 后台易失 + 旧进程占端口**：用 `netstat -ano` 找真占用者再 `taskkill`，不要盲目换端口。
- **Go 单发 channel 给已断开客户端会 panic**（send on closed channel）——发送前在锁内校验成员集。
- **Playwright `isVisible()` 对 `translate-x` 离屏元素误报 true**：断言用 `boundingBox()`。
- 部署宿主上普通用户可能无 `docker.sock` 权限 → `sudo -n docker ...`；本机缺 `sqlite3` 客户端时，用挂载 db 文件的 alpine 容器跑查询。
- **OIDC 本地开发不要复用生产 client**：localhost 和生产各注册一个 confidential client；本地 HTTP 需显式 `CHAT_COOKIE_SECURE=false`，生产保持 `true`。
- **Account 全局退出有两层**：Chat cookie 先清，Account end-session 再做全局撤销；`CHAT_OIDC_POST_LOGOUT_REDIRECT_URI` 必须指向 `/api/logout/account/callback` 且与 Account client 登记值逐字一致。发起退出时写入短时 HttpOnly state cookie，回调校验并消费后才展示成功状态。
- **Back-channel logout 不是只删 cookie**：按 Account `sub` 推进持久化 generation；REST、WS upgrade、join 和 Hub register 都必须重验。Live WS 先发 typed `account_logout` 再关闭，不能用裸断线替代。
- **OIDC 模式只接受 OIDC session**：启用 `CHAT_OIDC_ENABLED=true` 后，历史上签发的本地 Chat cookie 也必须失效，不能让切换开关留下第二身份面。

## 安全约定

- secret 不进仓：`.env*`、`.env.local` 均被 `.gitignore` 忽略，示例值只在 `.env.example`。
- 公开文档不出现服务器 IP、内部端口、SSH、凭据或生产数据。
- 敏感配置一律走环境变量；`CHAT_SESSION_SECRET`、`CHAT_LLM_API_KEY` 和 `CHAT_OIDC_CLIENT_SECRET` 必须在部署环境提供。Account access/refresh/ID token 只存在于 OIDC 回调处理期间，不进入浏览器、日志或数据库。
- 同源判断同时核对精确 scheme 与 Host；TLS 终止在 nginx 时，只有 `CHAT_TRUSTED_PROXY_CIDRS` 内的直连代理可以提供 `X-Forwarded-Proto`。
- 源码/运行配置边界与 AGPL 发布门禁见 `docs/source-boundary.md`；private repo 链接不算公开源码交付。

## 文档习惯

- 相对时间写绝对日期；文档引用 AGENTS 规则时写主题名或文件路径，不写编号式引用。
- 完成一个里程碑后请运行 `neat-freak` 知识卫生收口。
