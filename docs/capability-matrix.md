# 能力矩阵 Capability Matrix

> **本矩阵是仓库唯一的能力事实来源。** 每行描述当前可调用运行面（UI/HTTP/WS/数据写入），而非目标状态或已部署证明。最后更新：2026-10-21。

## 状态定义

| 状态 | 含义 |
|---|---|
| Core | 在用：当前产品合同启用并持续使用。 |
| Experiment | 实验：当前启用，但未完成最终产品裁决。**Experiment（retirement pending）** 表示该能力无已知真实消费者，倾向退休，由 #53 负责逐项裁决——验证问题、退出条件、owner 见 #53。 |
| Compat | 兼容保留：**必须存在真实旧消费者**，只允许必要读取和解码，不扩大也不新增写入。无真实消费者则不标 Compat。 |
| Archived | 已退休：运行面代码已删除，HTTP 请求返回 404，WS 不处理对应事件。 |

列说明：UI=当前前端主界面是否有入口；HTTP=当前后端是否注册 REST 路由；WS=当前后端 switch 是否处理 WebSocket 事件；数据写入=当前是否仍可产生数据写入（通过 WS 或 HTTP）；历史读取=历史数据是否仍可只读展示；构建包含=当前源码构建是否包含该处理器（仍受配置开关控制），不代表该版本已经部署。

## 矩阵

| 能力 | 状态 | UI | HTTP | WS | 数据写入 | 历史读取 | 构建包含 |
|---|---|---|---|---|---|---|---|
| 公共消息 | Core | 有（公共聊天室） | 有（/api/messages、/api/search、/api/export） | 有（message） | 有 | 有 | 是 |
| 私聊助手 1:1（TokenBot） | Core | 有（侧栏"私人助手"独立视图） | 有（/api/messages?to=，鉴权） | 有（message to=BotName + private stream） | 有（to_user 列） | 有 | 是 |
| 消息反应 | Core | 有（消息气泡 reaction 按钮） | 无 | 有（reaction） | 有 | 有 | 是 |
| 消息编辑/删除 | Core | 有（消息气泡编辑/删除菜单） | 无 | 有（message_edit、message_delete） | 有 | 有 | 是 |
| 引用回复/线程 | Core | 有（回复预览、ThreadPanel） | 无 | 有（thread_messages） | 有 | 有 | 是 |
| 消息搜索 | Core | 有（搜索栏入口） | 有（/api/search） | 无 | 无 | 有 | 是 |
| 语音输入 | Core | 有（空输入时显示麦克风，浏览器 SpeechRecognition；Permissions-Policy 仅放行同源 microphone，camera/geolocation 仍禁用） | 无（音频不经过 Chat 后端） | 无 | 无 | 无 | 是（浏览器支持时） |
| 邀请/分享 | Core | 有（宽屏邀请 pill、更多菜单；Web Share 或复制链接） | 无 | 无 | 无 | 无 | 是 |
| Agent mention / Reply（TokenBot） | Core | 有（输入框 mention、引用回复） | 无独立聊天生成路由 | 有（流式回复持久化） | 有 | 有 | 是（需 LLM 配置） |
| TokenBot 请求状态与停止 | Core | 有（排队/回复中；本人请求可停止） | 有（`/api/config` 返回 `capabilities.run_control`；无独立停止路由） | 有（`bot_run`、`bot_cancel`、带 run 标识的流式/最终回复） | 有（持久 run 与唯一最终消息；`send_message` 可发有界 interim） | 有（重连恢复可见状态，历史加载最终回复与 interim） | 是（需 LLM 与持久 run 存储） |
| TokenBot 会话队列与每日预算 | Core | 有（排队状态、队列满/预算错误提示） | 无独立管理路由 | 有（显式触发时接纳与校验） | 有（稳定主体、冻结输入、预算预留/结算） | 无独立账本读取入口 | 是（单实例 SQLite） |
| TokenBot 网页搜索/读取与来源 | Core | 有（按配置显示能力；来源读取状态与引用链接） | 有（`/api/config` 的 `capabilities.web_search`；无浏览器直连工具路由） | 有（现有 Bot 回复中的安全 `source` parts） | 有（run 来源、正文快照与独立工具预算） | 有（最终回复的来源列表） | 是（默认关闭，需独立 DeepSeek Search/Fetch 配置） |
| 同账号多标签页 | Core | 有（标签页共存，私聊与停止同步） | 有（既有本人私聊历史） | 有（同账号有效会话收取私聊回显、流式、状态与最终回复） | 沿用消息/run 写入 | 有（本人历史） | 是（已认证会话） |
| Account OIDC 登录与退出 | Core | 有（Account 登录；离开 Chat / 退出 Account 分开） | 有（`/api/oidc/config`、`/login`、`/callback`、`/backchannel-logout`、`/api/logout/account`、`/api/logout/account/callback`，按开关注册） | 有（WS upgrade 从 `tdchat_session` cookie 绑定身份；join 与 register 重验 logout generation；登出回调校验同浏览器 state cookie） | 有（`oidc_users`、`user_profiles`、logout epoch、JTI 去重） | — | 是（受开关控制） |
| 游客身份 | Experiment | 有（未登录自动 guest 加入，localStorage 持久化身份） | 无 | 有（join 不带 token） | 有 | 有 | 是 |
| 本地注册/登录 | Experiment | 仅 OIDC 关闭时显示（本地开发） | 仅 OIDC 关闭时可用（`/api/register`、`/api/login`） | 有（WS upgrade 从 `tdchat_session` cookie 绑定身份） | 有 | 有 | OIDC 模式下返回 404 |
| 自定义 Emoji | Experiment | 有（EmojiPicker 展示、上传、删除） | 有（/api/emoji/upload、/uploads/emojis/） | 有（custom_emoji_add/list/delete） | 有 | 有 | 是 |
| 投票 | Experiment（retirement pending） | 有（PollMessage 组件渲染、投票、关闭） | 无 | 有（poll_create/vote/close） | 可（WS 可写） | 有 | 是 |
| 聊天文件夹 | Archived | 无 | 无（404） | 无（WS handler 已删，2026-08-31） | 否 | 有（历史只读） | 否 |
| DM/群组 | Archived | 无 | 无 | 无（group_*/friend_*/dm_* WS handler 已删，2026-08-31） | 否 | 有（历史只读） | 否 |
| Webhook | Archived | 无 | 无（/api/webhook/ 路由已删，404，2026-08-31） | 无 | 否 | 有（历史只读） | 否 |
| 语音/视频通话 | Archived | 无 | 无 | 无（call_* WS handler 已删，2026-08-31） | 否 | 否 | 否 |
| 定时消息 | Archived | 无 | 无 | 无（schedule_* 已删，2026-08-31） | 否 | 否 | 否 |
| 转发 | Archived | 无 | 无 | 无（forward 已删，2026-08-31） | 否 | 有（历史标记只读） | 否 |
| Giphy 检索 | Archived | 无 | 无（路由已删除，404） | 无 | 否 | 否 | 否 |
| 普通文件上传 | Archived | 无（composer 无通用附件入口） | 无（通用路由已删除，404） | 无 | 否 | 否（历史上传文件不再可访问） | 否 |
| 图片消息 | Experiment | 有（加号菜单与粘贴，附件缩略图卡片） | 有（/api/upload/image、/uploads/images/，解码重编码，最大 4 MP；bot 以 image content parts 读图） | 无（HTTP 上传 + Markdown 图片） | 是 | 是 | 是 |
| GIF picker（前端） | Archived | 无 | 无 | 无 | 否 | 否 | 否 |

## 2026-10-21 本地增量与未实现能力

公开 Bot 的流式文字、最终 parts、新写入及旧历史读取仅公开文字与服务端注册的安全来源；原始 reasoning、搜索词、tool arguments/results 与网页全文不进入公共房间，旧数据库原文保留。引用/线程读写按真实父消息的会话归属校验，未知 `to` 返回错误，伪造回复作者和预览不作为可信元数据。

持久 run、停止、单条最终回复幂等发布、稳定主体预算与同账号多标签页已在本地实现。用户可停止自己排队或执行中的请求；其他房间用户不能代为停止。停止确认后不再显示该请求的未完成草稿，已提交的最终回复仍留在历史。Account 会话撤销同时使旧会话的 run 失效，私聊内容与状态只送达同账号的有效会话，公共事件只发往所属房间。

队列在单实例内按会话串行、按请求者轮转；输入在触发消息处冻结，后来消息不改变已入队的问题。SQLite 保存状态和每日预算，重启把未完成 run 记为失败，不自动重跑；重连恢复可见请求的近期状态与历史最终消息，不重放未完成片段。模型可在最终答案前通过 `send_message` 发布有界过渡消息（群聊 1 条、私聊 2 条），过渡消息不绑定 `run_id`、不改变唯一 final 的提交语义。预算在入队时预留，完整用量可结算；用量缺失或执行中停止保留预留，排队中停止释放 token 预留但仍计请求次数。该实现不提供分布式调度或后台持续任务，详细边界见 [Bot 架构](./bot-architecture.md)。

网页搜索/读取按配置启用：沿用当前模型与 SDK，独立调用 DeepSeek Search/Fetch。来源列表区分 `discovered`（仅发现）、`fetched`（已读取）和 `failed`（读取失败）；`cited` 只可标记本 run 成功读取并被答案引用的来源，普通 Markdown 链接不产生验证标记。服务端保存 provider 返回的有界正文快照、哈希与截断状态，不声称执行本地网页抓取或取得最终重定向 URL。每 run 最多 3 次外部调用，金额预占持久化且独立于 token 预算；详情见 [Bot 架构](./bot-architecture.md)。前端 583 tests、20 组浏览器回归及真实后端/本地 HTTPS provider 桩已通过；Chat Search/Fetch 尚待发布。

自适应接话、向运行中请求追加指令（steer）、一轮发布多条消息、多 Bot handoff、Python 与定时/事件任务均**未实现**，不标 Core/Experiment。时间和同会话聊天检索继续保留。Account IAM/成员/邮箱拟实施方案见 [跨项目设计](../../docs/identity/account-platform-design.md)。能力状态不代表已部署。

## 说明

- **公共聊天室 / 私聊助手分离**。2026-08-31 引入独立 1:1 私聊视图；2026-10-21 支持同账号有效标签页同步。`to=BotName` 的私聊不向公共房间或其他账号广播，历史经 `GET /api/messages?to=`（鉴权、按请求者作用域）加载。公共房间 `@TokenBot` mention 仍公开回复。
- **2026-10-20：Account OIDC BFF**。授权码由后端兑换，浏览器只保留 `tdchat_session` cookie；旧 `/api/oidc/exchange|refresh|redeem` 与浏览器 Account token 路径已删除。OIDC 启用时本地密码、注册和邀请路由退出运行面。
- **2026-10-21：Account logout 闭环**。本地退出与 Account 全局退出分开；back-channel Logout Token 按 `sub` 持久化世代失效，同一 `jti` 完全相同 claims 可幂等重试，不同 claims 拒绝。旧 OIDC cookie、未 join 的已升级 WS 和已注册 live WS 都不能跨过新世代。
- **2026-08-31：遗留 IM 运行面切除**。DM/群组、Webhook、通话、定时消息、转发、文件夹的后端 WS handler 与路由已删除（转 Archived）；公共 bot 只响应 `@TokenBot` 或回复 Bot 的消息，无关键词/概率抢答；私聊直接触发。
- **2026-08-30：收敛为 TokenBot 单 agent**：旧第二 Agent 工作区入口移除，`@TokenBot` 为唯一 assistant；`GET /api/config` 暴露 bot 名、模型与 LLM 启用状态，前端不再有假模型选择器。
- **普通文件上传已退休**：通用附件入口与路由已删除（2026-08-03），请求返回 404；历史上传文件不再可访问。**图片消息**是 2026-10-22 恢复的窄能力：仅 png/jpeg，最大 4 MP，服务端解码重编码后存储，登录 5MB / 游客 2MB，按 IP 限流；不恢复任意文件或目录浏览。2026-10-23 起 composer 以附件缩略图卡片呈现（可直接粘贴），TokenBot 通过 `CHAT_VISION_ENABLED`（默认开启）把本地图片引用转成模型 image content parts，单 run 最多 3 张。
- **自定义 Emoji 当前为活动状态**：EmojiPicker 展示 customEmojis、保留上传/删除入口、调用 `/api/emoji/upload`。后续需裁决：保留加固（Core）或退休（Archived）。
- **投票仍为 Experiment（retirement pending）**：后端保留 poll_*；由后续裁决保留或退休。
- 状态随代码事实更新：任何能力增删或运行面清理完成后，先更新本矩阵，再同步其他文档。
