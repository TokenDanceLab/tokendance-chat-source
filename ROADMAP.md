# TokenDanceChat ROADMAP

最后更新：2026-10-21 HKT

## 当前目标

TokenDanceChat 是 TokenDance 的轻量公共聊天产品；当前主线是 **Chat + Account**。

当前产品目标是打磨公共聊天室、TokenBot、实时协议、持久化和 Account 身份接入：

- 公共聊天室优先，保留 TokenBot 单 agent 入口；
- Telegram 级别的消息流动、对话人体工学、移动端交互质量；
- Account 作为唯一生产身份入口；
- 安全、可测试、可部署的工程基线。

`ROADMAP.md` 是面向未来 agent 的持久化目标账本。每次有意义的实现、验证、安全复核或范围决策后更新。

## 产品原则

1. **Chat + Account first**
   Chat 和 Account 是当前主线。AgentHub 已退出当前参考范围，不据其旧架构规划 Chat。跨项目用户、运营成员、IAM 与邮箱方案集中在 [Account 平台设计](../docs/identity/account-platform-design.md)，当前先服务平台用户与内部运营，不做多组织。

2. **轻量产品**
   应用应保持干净、可用和高效：主产品面只保留公共聊天室、TokenBot 单 agent。复杂联系人添加、联系人私聊、群组、语音/视频通话、GIF picker、定时发送、webhook 管理、普通文件上传等旧 IM 压力测试能力已退休；对应后端路由/事件已于 2026-08-31 从运行面删除，不再进入主界面合同。能力状态与退役清单以 [docs/capability-matrix.md](./docs/capability-matrix.md) 为唯一事实来源。

3. **Typed realtime protocol**
   新能力应使用显式 WebSocket message type、typed 前端 helper、store contract 和 focused tests。

4. **Security by default**
   Secret 应为一次性或在可行时脱敏；仅生产环境细节不进入公开文档；安全发现追踪于 `SECURITY.md` 和本 roadmap。

5. **Verified increments**
   每次代码变更需先过 focused check，再 broader verification，方可声称完成。

## 活跃工作流

| 优先级 | 工作流 | 目标 |
|---|---|---|
| P0 | 协议/存储加固 | 将 WebSocket event 和 SQLite 表视为当前聊天产品的可靠运行面；为安全敏感合约添加回归测试。 |
| P0 | 验证基线 | 保持 `go test ./...`、前端 focused tests、`pnpm exec tsc --noEmit`、`git diff --check` 全绿。 |
| P0 | Account 身份主线 | 保持 confidential OIDC BFF 为唯一生产登录路径：Chat 只持有 `tdchat_session` cookie。新消息记录服务端稳定主体，run 归属、停止授权和用户预算按该主体校验；Account `sub` 映射的 Chat handle 继续用于展示与既有历史。保持同账号标签页共存和撤销后的输出隔离；新增登录行为必须先补 OIDC 正反例。 |
| P1 | 轻量聊天 UX | 公共消息流、threads、reactions、通知、搜索、移动端人体工学和 AI 工作区切换；能力状态与退役清单见 [docs/capability-matrix.md](./docs/capability-matrix.md)。 |
| P1 | Telegram UX | 快速消息列表、干净输入、移动端手势、copy/reply/edit 人体工学、打磨过渡、媒体查看器质量。 |
| P1 | 单 Bot 交互 | 已有稳定主体、会话排队、冻结触发上下文、可停止 run 和单条最终回复幂等发布；持续验证重连、停止、预算和多人房间体验。追加指令与受控主动接话仍待规格和后续实现；当前只有显式触发。 |
| P2 | 运维/性能 | Health check、部署 checklist、bundle/runtime profiling、虚拟列表调优、WebSocket fanout/load check。 |
| P2 | UI/美术方向 | 统一字体、Logo 规则与基础组件；中性色表面保留产品既有强调色，维持克制聊天界面。 |
| P2 | Bot 工具与协作规格 | DeepSeek 独立 Search/Fetch、来源列表与工具预算已实现，按配置启用并独立验收发布；Python runner、专家异步 handoff 与持续任务仍待后续规格和实现。 |

## 后续产品任务

1. 继续收紧公共房间、TokenBot 单 agent 的消息渲染、流式回复、搜索、reaction、thread 和移动端输入体验。
2. 保持 transcript、message block、composer 和 assistant workbench 的产品质量；新增 UI 必须先补 token/视觉验收。
3. 性能 pass：消息列表 profiling、bundle/chunk review、WebSocket fanout/load check。
4. 保留已退役 IM 的必要历史证据，不将已删除的协议描述为兼容运行面；不要把群组、真人 DM、通话、GIF、定时、转发或 webhook 管理重新接回主界面。
5. TokenBot 的实施顺序和验收边界见 [Bot 架构](./docs/bot-architecture.md)；在已完成的 run 控制基础上推进后续规格，研究结论不代表已上线能力。

## 2026-10-21 本轮增量

- 已在本地实现并完成后端全量 race、vet、build：公开 Bot 实时/最终/历史内容过滤；引用与线程的会话权限校验、可信回复预览；未知私聊收件人拒绝，防止回落公开房间。旧数据库原文保留，读取时过滤。
- TokenBot run 控制已在本地实现：可查看排队/回复中状态、停止本人请求、在同账号有效标签页同步私聊和停止操作；停止后的迟到片段不恢复草稿，已完成历史不被撤回。前端停止入口由 `/api/config` 的 `capabilities.run_control` 控制。
- 单实例 SQLite 保存 run 与预算；同一会话串行、不同请求者轮转，触发消息及其上下文入队即冻结，每个 run 最多写入一条最终回复。用户预算按稳定主体累计，token 先保守预留再按完整用量结算，重启和跨日不清除原有计费；重启将未完成 run 标为失败，不自动再调用模型。
- 后端全量测试与 hub/store/handler/llm race 已通过；独立复核发现的队列后继饥饿、部分用量误退款、完成提交后退出的状态通知及配置嵌套问题已补回归，并通过相关 race 检查。最终前端 563 项测试通过；8 组明暗/中英文/桌面移动端浏览器检查通过；真实 Chat 后端与本地模型桩的 10 项队列/取消/持久化/隔离检查及 5 项展示复验通过。集成使用本地测试身份，不代表生产 Account OIDC 或真实模型网关验收。
- Account 登录文案对比度与窄屏溢出修复通过 12 组浏览器场景（1440/390/320px × 明暗 × 中英）。Logo 资源、侧栏暗色品牌色修复与线程 UI 配套已完成最终验收：登录 E2E 12/12、视觉 13/13，结果见 [视觉验收](./docs/visual-acceptance.md)。
- Search/Fetch 增量已实现：保留 `deepseek-v4.1-flash`、openai-go v1.12.0 和 Chat Completions，通过 SDK `Client.Post` 调用独立 DeepSeek `/v1/tools/search`、`/v1/tools/fetch`。网页由 provider 获取；Chat 保存有界 Markdown 快照与来源状态，公共输出扩展为文字 + 安全来源，原始搜索词/工具结果/全文仍不公开。
- 来源与工具金额绑定持久 run；成功读取的注册来源才可获得服务端引用，流式来源标记隐藏，刷新从历史恢复来源。每 run 最多 3 次网页调用，按版本费率 ¥0.005/次预占、默认每日应用预算 ¥5；未知消耗保守保留，最终计费以网关账单为准。
- Search/Fetch 前端最终验证：34 文件 / 583 tests、typecheck、build 通过；lint 为 0 error / 47 既有 warning，浏览器 20/20 通过（1440/390/320px、明暗、中英及 run 回归）。真实 Go Hub + SQLite + 本地 HTTPS provider 桩验证 Search → Fetch → 引用、刷新和公私聊隔离；该结果不冒充生产 provider 验收。Service Worker 已升至 v12。
- 网关已通过官方 channel 验证 Search/Fetch 两项路由；Chat Search/Fetch 尚待提交与生产发布。发布状态依据 [RELEASE.md](./RELEASE.md) 与部署验收记录。自适应接话、追加指令、多消息发布、多 Bot、Python 和后台定时/事件任务仍未实现。

## Review Gates

提交或交接有意义的变更前：

- `git diff --check`
- `cd backend && go test ./...`
- `cd frontend && pnpm exec tsc --noEmit`
- 涉及文件的 Focused 前后端测试
- 文档更新（protocol、security、用户可见行为和能力矩阵）
- `tmp_*` 或无关本地文件不暂存

## 已完成基线

- 历史核心聊天快照：公共房间、DM、群组、好友、reactions、在线状态、typing。当前前端主合同只保留公共房间、TokenBot 单 agent。
- 历史数据完整性快照：SQLite 持久化、离线 DM、历史 reactions、消息上限、作用域 typing。当前 UI 只消费公开房间与基础消息状态。
- 当前 IM 打磨：未读角标、草稿、滚动记忆、搜索跳转、流式节流、中文 mentions、CSP/XSS 加固；转发、群组、通话、GIF/sticker、webhook 管理不再进入主界面。
- 高级功能：已读回执、最后在线、@mention 通知、通知声音、屏蔽。
- 进阶历史 IM：置顶/书签、群组邀请流程、threaded replies、范围搜索、无限历史、typing 预览、自定义 emoji。当前只保留能服务轻量公共聊天室的子集。
- 平台：PWA shell、前端单元测试、后端 WebSocket/store 测试、无障碍基线、Bot/Agent mention 路由。
- 2026-10-20：TokenDance Account OIDC 收敛为 confidential BFF 单一路径；授权码由 Chat 后端兑换，浏览器只持有 `tdchat_session` HttpOnly cookie，REST/WS 共用同一身份，旧 exchange/refresh/redeem 路由已删除。
- 2026-08-30：收敛为 TokenBot 单 agent（旧第二 Agent 工作区入口移除）；配置链修复——后端启动自动加载 `.env.local` 再 `.env`（OS 环境变量优先，文件只补缺）、`docker-compose.yml` 补传 `CHAT_LLM_*`、新增 `GET /api/config`；UIUX 修复包——设置抽屉背景改实、移动端 composer 修复、删除 composer 假「+」按钮、桌面/移动端明暗主题修复。
