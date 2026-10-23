# TokenDanceChat — 项目 Agent 交接 SSOT

> 本文件是项目级 Agent 交接的唯一事实来源（SSOT）：接手 TokenDanceChat 的 agent 先读本文件。
> TokenBot 的 system prompt 由 `backend/hub/prompt.go` 的 `BuildPrompt` 生成（版本 `TokenBotPromptVersion`），按模式静态、可缓存；对话记忆直接取自 SQLite（`backend/hub/history.go`）。本文件不参与 bot 上下文。

最后更新：2026-10-21 HKT

## 项目定位

TokenDanceChat 是 TokenDance 的轻量公共聊天产品。当前主线是 **Chat + Account**：

- Chat 负责公共聊天室、TokenBot、实时协议和产品内授权；
- TokenDance Account 是唯一生产身份入口；
- AgentHub 已退出当前参考范围；不作为设计、架构、实现或发布依据。

代码采用 AGPL-3.0-only；当前开发仓为 private，部署必须另行提供公开、可下载且与运行版本一致的 Corresponding Source。当前产品目标：

- Go Hub Server + WebSocket typed events 承载公共消息、presence、reactions、编辑、线程与 AI 流式回复；
- SQLite（modernc.org/sqlite，pure Go）+ FTS5 支撑早期 Hub 持久化与全文搜索；
- React 19 + Zustand + Vite 的客户端状态模型跑通轻量聊天工作台；
- 公共协议 mention 能否让 AI 以 IM 参与者的形态自然嵌入；
- TokenDance Account OIDC 作为统一身份入口接入 IM；Chat 只做 confidential BFF，不维护第二套生产账号。

能力状态与退役清单以 `docs/capability-matrix.md` 为唯一事实来源；
跨项目身份与后台设计见 [Account 平台设计](../docs/identity/account-platform-design.md)；
`docs/agenthub-validation.md` 仅说明历史沿革，不指导当前工作。

## 技术栈

| 层 | 技术 |
|---|---|
| 前端 | React 19 · Vite 6 · Tailwind CSS 4 · shadcn/ui (Radix Rhea) · AI Elements · Zustand 5 |
| 后端 | Go 1.26.10 · gorilla/websocket · net/http |
| 数据库 | PostgreSQL（pgx，生产）/ SQLite + FTS5（本地与单机兜底） |
| 实时协议 | WebSocket typed events |
| 后台任务 | River（嵌入 Go 进程，PG 持久化；每日 maintenance 清扫 90 天未用游客绑定与过期记忆） |
| LLM | `github.com/openai/openai-go` v1.12.0（Chat Completions；DeepSeek / Gateway）；独立 DeepSeek Search/Fetch adapter |
| 部署 | Docker · Nginx |

## 目录地图

```
backend/            # Go 后端
  handler/          # HTTP + WS handler + Account OIDC BFF + GET /api/config
  hub/              # 聊天核心（client / room / broadcast）
  store/            # SQLite + FTS5
  llm/              # OpenAI-compatible Chat Completions、工具循环与公开输出投影
  websearch/        # DeepSeek 独立 Search/Fetch API、URL 检查与有界响应
  main.go           # 入口：配置校验、路由注册、服务启动与优雅关闭
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
- 图片消息是窄能力：composer 加号菜单与直接粘贴（png/jpeg）都走 assistant-ui 的附件适配器，输入区显示缩略图卡片（可逐张移除），发送时才写成 Markdown 图片；服务端解码重编码去除元数据后存 `/uploads/images/`，登录 4MB / 游客 2MB，按 IP 限流；不恢复任意文件上传或目录浏览。TokenBot 的输入会把本地图片引用转成 OpenAI `image_url` content parts（`CHAT_VISION_ENABLED` 默认开启，单 run 最多 3 张、单图 ≤4MB、合计 6MB、只读 `/uploads/images/` 下的 png/jpeg，Base64 不落库），模型因此能直接看图；关闭后图片退化为 `[图片]` 文本占位。
- WebSocket 重连前两次静默（2s、4s 退避），只有持续失败才在会话内提示；界面连接态延迟 1.5s 再显示「连接中」，避免短暂抖动闪文案。
- 宽屏（≥1368px）为三栏：可收起为 72px 图标轨的 240px 会话栏、768px 阅读列、360px 信息栏；信息栏顶部为身份卡，下面三段 tab——群聊为成员/动态/助手，私聊为动态/来源/对话。阅读区不显示标题/状态横条，只在右上保留浮动控制组；1280–1367px 只保留会话栏与阅读列。
- 消息气泡参考 Muse：自己的消息用品牌蓝（浅色浅蓝底深字、深色饱和蓝白字），助手用中性气泡，22px 圆角；输入区为 28px 胶囊，运行中发送键切换为停止。
- 私人助手 1:1：侧边栏选 TokenBot 进入独立私聊视图；消息 `to=BotName` 走私聊通道（`Message.Private`），输入回显、流式回复、run 状态与最终消息只同步给同一账号的有效会话；历史经 WS `load_private`（按连接身份作用域）加载——游客由 HttpOnly 游客 cookie 标识（WS 与 HTTP 都能识别），HTTP `?to=` 仍只服务登录用户，游客也能在刷新后取回会话。公共与私聊消息流互不可见。私聊支持多线程（Muse 式「主聊天 + 旁聊」）：`messages.thread_id` 复用为私聊会话道，主聊天为 `''`（全部历史自动归入），旁聊为生成 ID；侧栏「旁聊」分组可新建/切换，run 按 `账号+会话道` 排队且模型上下文只含当前道。未查看时按会话道累计未读徽标与标题计数，系统通知只提示「有新回复」，不暴露内容。私聊时间线采用无身份噪音的极简呈现：无头像、无用户名、无逐条时间与回执；同一会话内超过 5 分钟的间隔插入居中时间刻度，跨天显示日期分隔。composer 支持直接粘贴 png/jpeg 上传（与加号菜单同一受保护管线：服务端重编码去元数据，登录 4MB/游客 2MB）。
- 显式长期记忆（P0，`CHAT_MEMORY_ENABLED` 默认开）：登录账号在私聊里明确要求「记住…」时，模型可调用 `remember_fact`（默认 `lane`，只有用户明说「所有对话/以后都」才允许 `actor`；每 run 最多 1 条），事实存 `agent_memories` 并在后续私聊 run 的 trigger turn 前部以 `[Memory context …]` 数据块注入（不写入 `bot_runs.input_json`）；服务端要求触发消息含明确「记住」意图才允许写入，`forget_fact` 要求「忘掉」意图且每 run 一次，规则覆盖与 secret 过滤对所有 kind 生效。公共房间不写入也不注入，游客不写记忆，secret 与试图覆盖规则的指令由服务端拒绝。自动抽取、记忆管理界面、房间共享记忆是 P1/P2。
- 后台任务（P1，`CHAT_RIVER_ENABLED` 默认开，仅 PostgreSQL）：River 客户端嵌入同一 Go 进程，使用独立 queue 与 migration；当前只有每日 `maintenance`（删除 90 天未使用的游客身份绑定、把过期记忆标记为 expired）。River 自己的 schema 在迁移阶段用 DDL 角色创建，运行角色只做 DML。
- TokenBot：单 bot，LLM 流式回复；模型由服务端 `CHAT_LLM_MODEL` 配置，前端通过 `GET /api/config` 展示真实模型名，前端不假设模型列表。
- 公共聊天室可显式开启主动参与（`CHAT_BOT_AMBIENT`，默认关闭）：没有 @ 时由一个无工具的轻量判断决定是否接话；同一房间的连续消息先聚合成一批（安静 8 秒触发、最长 30 秒硬上限），再受每房间冷却、每房间每小时上限和“同房间已有进行中 run”约束；显式 @/回复 Bot 永远优先，判断只记录动作与理由摘要，不记录消息正文。
- 每次获接纳的显式触发生成持久 run；UI 显示排队/回复中，发起者可停止自己的请求，也可在同账号的其他有效标签页停止。停止确认后保留已流出的部分回复为「已停止生成」终态消息，可复制或重试原请求；未收到任何片段时显示一行停止提示。已提交的历史保留；断线时停止按钮不可用，不缓存停止指令等重连后发送。失败会清空未完成草稿并追加一次性提示，提示可直接重试原请求。
- 同一房间或同一账号私聊每次执行一个 run，按请求者轮转且保留各自先后顺序；不同会话共享有界计算槽。触发消息和截至该消息的上下文在入队时冻结，之后聊天不改写正在回答的问题；每个 run 的最终答案是一组幂等消息：模型可用单独成行的 `<msg-break>` 把它拆成一小串连续短消息（群聊 ≤3 条、私聊 ≤4 条），整组与 run 完成在同一事务落库，首条替换流式草稿；模型也可在最终答案前通过 `send_message` 发布有界过渡消息（群聊 1 条、私聊 2 条，≤600 字符），过渡消息不绑定 `run_id`。
- SQLite 持久化 run、稳定账号主体与每日预算。队列属于单实例本地运行面；重启将未完成请求标为失败，不自动重跑。重连补发可见 run 的近期状态，最终回复从消息历史恢复，已流出的未完成片段不重放。
- 发送被服务端拒绝时，错误事件携带 `client_message_id`，前端把对应乐观消息标记为失败并提供重试；run 以 `failed` 结束时在会话内追加一条一次性提示，不静默丢弃请求。历史响应区分分页与连接快照：只有 `load_history` 的响应带 `pagination`，客户端按时间序合并快照，不得把重连快照前插到旧窗口之上。
- `GET /api/config` 的 `capabilities.run_control` 在持久 run 存储与 LLM 可用时为 `true`；前端据此启用停止入口，无顶层 `run_control`。本轮实现状态见能力矩阵，发布状态另以 `RELEASE.md` 和部署验收记录为准。
- Bot 的触发、记忆边界、prompt 缓存与工具暴露以 `docs/bot-architecture.md` 为准；改 prompt 或工具签名要同步 bump `TokenBotPromptVersion`。
- 公共 Bot 输出仅有文字与服务端注册的安全来源 DTO（`source_id/title/url/status/cited`）；reasoning、原始 query、工具参数/结果和网页全文不可通过实时、最终 parts 或历史接口发送到房间。私聊仍按账号隔离，前端也隐藏原始 Web 工具卡片。
- 配置启用后，TokenBot 可调用 `web_search` 发现网页、`web_fetch` 读取网页，保留 `deepseek-v4.1-flash` 和现有 SDK；使用独立 DeepSeek `/v1/tools/search`、`/v1/tools/fetch`，不等同于任意模型支持 Responses 内置搜索。`capabilities.web_search` 依据实际 adapter、run 存储和配置返回。
- 来源绑定本 run 并持久化。列表区分已读取、仅发现和读取失败；只有成功 fetched 的来源引用标记可由服务端转换成链接，普通 Markdown 链接不获得引用标记。流式阶段隐藏内部来源标记，取消/退出后不允许迟到来源恢复草稿。
- Fetch 由 provider 完成，Chat 只保存有界 Markdown 快照、哈希和截断标记，不声称掌握最终重定向 URL。输入 URL 受 `SafeURL` 校验，provider 内部 DNS/重定向由其负责。外部工具调用按版本费率单独持久预占，每 run 最多 3 次、默认每日应用预算 ¥5；这不等于网关账单或精确换汇限额。
- 引用和线程按数据库中的真实父消息检查会话作用域；不存在、已删除和不可见引用返回相同通用错误。回复预览与 Bot 定向作者从真实消息派生，未知 `to` 拒绝发送，不回退公共房间。
- 主动参与的 `ask` 动作与按房间灰度、向运行中请求追加指令（steer）、多消息分类发布（delivery_key 许可体系）、多 Bot、Python 和后台定时/事件任务仍是后续规格；现有时间与同会话聊天检索继续保留。实施顺序见 `docs/bot-architecture.md` 与 `ROADMAP.md`。
- 其它历史 IM 能力（DM、群组、语音/视频、GIF、定时发送、转发、webhook 管理、普通文件上传、文件夹）已于 2026-08-31 从后端运行面删除（Archived）。若要恢复，必须先更新 `ROADMAP.md` 并补足视觉/E2E 证据。

前端复用 assistant-ui、`frontend/src/components/ui` 与 `frontend/src/components/ai-elements` 的现成组件和语义 token，不新增并行设计系统、旧 `--td-*` 别名或自定义动画 keyframes。TokenDance 产品统一字体、Logo 规则和基础组件；页面以中性色为底，保留各产品既有品牌强调色，不要求全产品同色。资产与用法以 `DESIGN.md` 路由的品牌契约为准。Muse 对照的逐项覆盖与刻意不做的能力见 [`docs/muse-parity.md`](docs/muse-parity.md)。

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
- **本地临时服务**：只监听 loopback；旧进程占端口先用 `ss -ltnp` 确认进程与端口，不盲目换端口或终止其他任务。
- **Go 单发 channel 给已断开客户端会 panic**（send on closed channel）——发送前在锁内校验成员集。
- **Playwright `isVisible()` 对 `translate-x` 离屏元素误报 true**：断言用 `boundingBox()`。
- 部署宿主上普通用户可能无 `docker.sock` 权限 → `sudo -n docker ...`；本机缺 `sqlite3` 客户端时，用挂载 db 文件的 alpine 容器跑查询。
- **OIDC 本地开发不要复用生产 client**：localhost 和生产各注册一个 confidential client；本地 HTTP 需显式 `CHAT_COOKIE_SECURE=false`，生产保持 `true`。
- **Account 全局退出有两层**：Chat cookie 先清，Account end-session 再做全局撤销；`CHAT_OIDC_POST_LOGOUT_REDIRECT_URI` 必须指向 `/api/logout/account/callback` 且与 Account client 登记值逐字一致。发起退出时写入短时 HttpOnly state cookie，回调校验并消费后才展示成功状态。
- **Back-channel logout 不是只删 cookie**：按 Account `sub` 推进持久化 generation；REST、WS upgrade、join 和 Hub register 都必须重验。Live WS 先发 typed `account_logout` 再关闭，不能用裸断线替代。
- **同账号标签页不是重复登录**：有效的已认证连接应共存；run 授权和用户预算使用稳定主体，不信任显示 handle。Account 撤销时一并终止旧世代 run，后续模型/工具输出不得继续发布。游客同理：同一 HttpOnly 游客 cookie 的多标签连接共存，不同 cookie 不能占用已绑定 handle。
- **OIDC 模式只接受 OIDC session**：启用 `CHAT_OIDC_ENABLED=true` 后，历史上签发的本地 Chat cookie 也必须失效，不能让切换开关留下第二身份面。
- **游客私聊不是隐私边界**：游客身份是浏览器 cookie 级的弱身份：`/api/session` 签发 HttpOnly、SameSite=Lax、一年期的签名游客 cookie（生产为 `__Host-tdchat_guest`，128-bit 随机值 + HMAC；升级期接受已绑定的旧版未签名值并原地重签），首次 join 的 handle 绑定到该 cookie，同浏览器跨刷新/多标签复用；绑定永不复用给其他浏览器，有私聊历史的 handle 不再被新 guest 认领，OIDC/local 用户名分配也避开已绑定 handle；换设备、清 cookie 或 Account 登录（登录会清空游客 cookie）即新身份。它不提供密码、恢复或跨设备能力，游客私聊历史对能拿到该 cookie 的人可见，涉及隐私内容的对话必须登录 Account。

## 安全约定

- secret 不进仓：`.env*`、`.env.local` 均被 `.gitignore` 忽略，示例值只在 `.env.example`。
- 公开文档不出现服务器 IP、内部端口、SSH、凭据或生产数据。
- 敏感配置一律走环境变量；`CHAT_SESSION_SECRET`、`CHAT_LLM_API_KEY` 和 `CHAT_OIDC_CLIENT_SECRET` 必须在部署环境提供。Account access/refresh/ID token 只存在于 OIDC 回调处理期间，不进入浏览器、日志或数据库。
- 同源判断同时核对精确 scheme 与 Host；TLS 终止在 nginx 时，只有 `CHAT_TRUSTED_PROXY_CIDRS` 内的直连代理可以提供 `X-Forwarded-Proto`。
- 源码/运行配置边界与 AGPL 发布门禁见 `docs/source-boundary.md`；private repo 链接不算公开源码交付。

## 文档习惯

- 相对时间写绝对日期；文档引用 AGENTS 规则时写主题名或文件路径，不写编号式引用。
- 完成一个里程碑后请运行 `neat-freak` 知识卫生收口。
- 内部过程材料（交接文档、任务书、调研草稿、临时分析）写 `.local/`，不提交进仓；仓内文档只保留对外长期有效的产品事实与契约。

## 并行开发与舰队分工

多线并行时按此分工，每条线独立 worktree（`.worktrees/<线名>`），任务书写明文件归属；共享文件（本文件、README、i18n 索引、能力矩阵）由集成者统一改，线内不碰。

- **前端/UIUX 线**：拥有 `frontend/src/components/` 与视觉面。设计参照系以 muse.ai 为主（简洁、克制、气泡/动效细节）；每次有意义的视觉变更必须附真实浏览器截图对比，走 `docs/visual-acceptance.md` 流程。
- **后端线**：拥有 `backend/`。架构按 `docs/` 与现行 ADR；不引入未经设计评审的新依赖。
- **测试线**：拥有 `*.test.ts(x)` 与 `e2e/`。UIUX 类问题必须能提前被自动化发现——视觉/几何断言进 e2e 或视觉验收场景，不依赖人肉看截图；关键流程同时保留手动复核。
- **安全线**：只读审查 + 报告，不改代码；审查面覆盖鉴权/配额/外部写入/注入面/数据隔离。发现的问题回集成者处置。
- **浏览器巡检与截图验收**：复用单一标签页（同一时间最多保留 1 个在用的本地构建标签），结束立即关闭自建标签；不要把每个场景开成新标签，Playwright 用独立 headless context，不占用户标签。
- **集成者（主线）**：出任务书、验收、合并、部署与文档收口。

