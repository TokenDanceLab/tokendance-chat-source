# 工程目标

> 面向未来的 agent 与维护者：TokenDanceChat 是独立运行的轻量公共聊天产品。当前主线是 Chat + Account；AgentHub 是未来的可选集成与复用参考，不是当前依赖或优先级。

## 当前目标

将 TokenDanceChat 推进为轻量、可靠、可部署的公共聊天产品，并用 TokenDance Account OIDC 作为唯一生产身份入口。当前前端产品合同只包含公共聊天室与 TokenBot 单 agent；早期 rich IM 运行面已归档，仅保留历史读取、迁移知识和安全证据。

这意味着：

- 保持 Chat 与 Account 的身份、会话和产品授权边界稳定；
- 保持轻量聊天产品可用、打磨到位、可部署；
- 优先类型化协议契约、经过测试的 store 行为以及可恢复的 UI 状态，而非一次性 UI 补丁；
- 每次代码变更均以聚焦测试加对应层级的 build/类型检查加以验证。

## 设计原则

1. **Chat + Account 优先**
   Chat 和 Account 是当前主线。AgentHub 只在明确排期时作为可选集成与复用参考，不能阻塞当前产品开发。

2. **轻量产品**
   应用应保持公共房间与 TokenBot 单 agent 的核心体验稳定。DM、群组、通话、GIF、转发、定时发送、webhook 管理等旧能力已经完成压力测试，不再进入当前前端主合同。

3. **类型化 realtime 协议**
   新增 realtime 功能应通过显式 WebSocket 消息类型、类型化前端 API helper 以及 store 方法流转。避免隐藏的 ad hoc 事件数据。

4. **SQLite 作为当前聊天事实来源**
   持久化应保持显式、可迁移、受 store 测试覆盖。若某功能变更持久化状态，需在展开 UI 工作前定义表/store 契约。

5. **小步验证增量**
   倾向带有回归测试的窄范围变更。UI 行为尽可能增加 Vitest 覆盖；协议/store 变更则围绕 store、handler 或 hub 边界增加 Go 测试。

6. **禁止敏感运维信息外泄**
   公开文档可描述部署形态，但不得包含服务器 IP、内部端口、SSH 细节、凭据或生产数据。

7. **多模态 UI 验收**
   有意义的前端打磨必须通过真实浏览器截图与指标加以验证。生成的 `gpt-image-2` 参考图可引导视觉方向，但不可作为验收证据。

## 工作流

| 优先级 | 工作流 | 重要性 |
|---|---|---|
| P0 | 产品与身份边界 | 保持 Chat 产品授权与 Account 身份、OIDC session 的边界稳定。 |
| P0 | 协议与 store 加固 | WebSocket 与 SQLite 契约是当前聊天产品的可靠性基础。 |
| P0 | 验证基线 | 无法测试或稳定构建的产品不能发布。 |
| P1 | Agent 入口体验 | 通过 TokenBot 单 agent 的 mention 和 assistant workbench 提供真实助手能力。 |
| P1 | 历史 rich IM 证据 | 旧群组、webhook、通话、角色及通知不再提供运行接口；只维护历史读取、迁移知识和安全回归证据。 |
| P1 | 前端状态与组件清理 | 大文件 UI 可逐步收敛为可维护、可测试的组件边界。 |
| P2 | 产品打磨 | 改善真实用户体验，但不扩大当前产品合同。 |

## 当前高价值后续步骤

1. 持续打磨消息渲染、聊天区和输入框的可靠性、可访问性和移动端体验。
2. 保持公共房间与 TokenBot 单 agent 的发送、刷新持久化、移动端布局和无旧入口 E2E 覆盖。
3. 将 `MessageBubble.tsx` 的剩余历史逻辑按测试保护逐步拆小，composer 与对话流继续以 assistant-ui primitives 为唯一实现，不恢复旧 rich IM UI。
4. 身份和会话变更必须同步 Account OIDC 契约、负向测试和安全文档。
5. 每个里程碑后保持 `AGENTS.md`、README、ROADMAP 与 `docs/capability-matrix.md` 同步。

## 必要验证

使用能证明变更的最小命令，声明完成前再运行更广泛的检查。

| 变更类型 | 最低验证 |
|---|---|
| Go 后端/store/协议 | `cd backend && go test ./...` |
| 前端组件/store/API | `cd frontend && pnpm test -- --run <聚焦测试>` 外加 `pnpm exec tsc --noEmit` |
| 前端构建/运行时面 | `cd frontend && pnpm run build` |
| 前端视觉打磨 | `cd frontend && pnpm run visual:acceptance`，然后检查截图输出目录 |
| 仅文档 | 阅读变更后的文档检查链接/路径，并 `git diff --check` |
| 部署 | 重启后健康检查，以及与变更相关的 WebSocket/手动冒烟测试 |

## 决策规则

在两个下一步任务之间选择时，优先选择最能满足以下排序的任务：

1. 修复正确性、安全性或数据完整性风险；
2. 强化 Chat + Account 的产品边界与核心可靠性；
3. 保持产品可部署、可测试；
4. 改善真实用户体验；
5. 降低未来维护成本。
