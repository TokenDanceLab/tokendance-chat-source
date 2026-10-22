# Muse 交互对照与覆盖边界

最后核对：2026-10-22　生产版本：`d186ae9`（生产主机本地构建镜像；GH Actions 账单阻塞期间）
证据：`docs/visual-acceptance.md`、`frontend/src/**/*.test.ts(x)`、`backend/hub/*_test.go`、线上 1440×900 / 1300×900 / 390×844 实测。

这份文档回答“Muse 对照做到哪一步”。它按可见交互面逐项列出实现状态，并把不属于聊天产品的部分标成明确边界，避免把“未实现”和“未对齐”混在一起。

## 已覆盖

| Muse 面 | 本产品实现 | 证据 |
|---|---|---|
| 三栏工作台 | 240px 会话栏（可收为 72px 图标轨）、768px 阅读列、360px 信息栏（≥1368px） | `ChatLayout.tsx`、视觉验收 |
| 无顶栏阅读区 | 宽屏不渲染标题/状态横条，仅右上浮动控制组；标题留在侧栏和信息栏 | `ChatLayout.tsx`、1440 实测 |
| 右侧身份与 tab | 30/360 信息栏：身份卡 + 成员/动态/助手 或 动态/来源/对话；滑片高亮 | `ChatInfoRail.tsx`、rail 测试 |
| 活动时间线 | 按天分组、40px 图标块、14/20 标题、13/18 描述、12/16 时间 | Muse 实测值、活动测试 |
| 消息气泡 | 自己 `#B8DBFE`/深色 `#0074E8`，助手 `#E9EAEB`/`#1F1F20`，22px 圆角、11px 15px 内边距、16px 正文 | `MessageBubble.tsx`、视觉验收 |
| 分组与 hover 操作 | 同作者分组、贴合边收 6px；26px 圆形操作贴气泡侧边居中，150ms 淡入 | `MessageBubble.tsx` |
| 胶囊 composer | 32px 圆角、56px 高、加号菜单（表情/图片）、模型 chip、发送/停止 | `chat-thread.tsx`、视觉验收 |
| 语音输入 | 空输入显示麦克风，浏览器 `SpeechRecognition` 识别，输入后切回发送 | `useSpeechInput.ts` + fake-engine E2E |
| 邀请/分享 | 宽屏邀请 pill；桌面复制链接，触屏调起系统分享；窄屏在更多菜单 | `ChatLayout.tsx`、线上 toast 实测 |
| 回复/引用 | composer 上方可取消引用 chip，切换会话清除，预览去 Markdown | `chat-thread.tsx`、`plainTextPreview` 测试 |
| 设置抽屉 | 账号/外观/助手/通知/关于；主题、语言、推理开关、通知权限、静音预览 | `SettingsPanel.tsx`、设置测试 |
| 动效 | tab 滑片 200ms、内容 fade 150ms、消息进入、hover 操作；尊重 `prefers-reduced-motion` | `index.css`、组件 |
| 深色主题 | 画布 `#181819`、卡片/浮层/侧栏上移一档、助手气泡 `#1F1F20` | `index.css`、浅深实测 |
| 流式回复 | 三点气泡、思考过程折叠、工具卡片、停止/排队状态 | `chat-thread.tsx`、流式实测 |
| 停止/失败终态 | 停止保留已流出片段并标「已停止生成」，可复制或重试原请求；无片段时显示一行停止提示；失败提示同样可重试 | `botRunState.ts`、`MessageBubble.tsx`、终态测试 |
| 私聊未读与通知 | 未查看该助手时累计会话徽标与标题计数；系统通知只提示「有新回复」，不暴露私聊内容 | `useWebSocket.ts`、`LightChatSidebar.tsx`、通知测试 |
| 多次回复 | `send_message` 过渡消息（群 1/私 2，≤600 字符），final 仍唯一 | 后端集成测试、生产「收到 E2E」双气泡 |
| 主动工具调用 | 时间、会话检索、`web_search`/`web_fetch`、来源与引用状态 | `hub/tools.go`、`hub/web.go`、来源实测 |
| 无需 @ 的主动参与 | 无工具 selector 判断 + 冷却/每小时上限/独占 run；显式 @ 优先 | `hub/ambient.go`、生产实测 |
| 移动端 | 信息栏隐藏、侧栏 Sheet、composer 44px 触控、建议胶囊 44px | 390×844 实测 |
| 长按/上下文菜单 | 长按打开完整菜单；未启用选择模式时隐藏“选择” | `MessageBubble.tsx`、长按测试 |
| 已读回执 | fixed 弹层，避免消息行裁切 | `MessageBubble.tsx` |
| 重连稳定性 | 前两次静默、2s→60s 退避、1.5s 连接态延迟、快照 `trigger_preview` | `useWebSocket.ts`、`useConnectionSettled.ts`、生产实测 |

## 明确边界（不是待办 UI）

| Muse 能力 | 本产品决定 | 原因 |
|---|---|---|
| 远程浏览器实际操作网站 | 不做；保留只读 `web_search`/`web_fetch` | 需要独立沙箱浏览器服务、域名/凭据策略、审计与资源隔离；不能把个人 antidetect profile 或共享浏览器接进公共聊天 |
| 服务端语音识别 | 不做；用浏览器原生识别 | 避免把用户音频上传到 Chat 后端；能力取决于浏览器和麦克风权限 |
| 邀请记录/成员管理 | 不做；只分享房间链接 | 当前是公共聊天室，没有邀请码、成员审批或群组管理模型 |
| Muse 的个人电脑、任务/笔记/应用侧栏 | 不做 | 属于个人 Agent 操作系统，不是聊天室产品面 |
| 账号资料编辑、头像上传 | 不做 | 身份与资料由 TokenDance Account 承担 |

## 维护规则

- 新增或移除 Muse 对照项时同步本表和 `docs/visual-acceptance.md`，不要把“刻意不做”写成“尚未实现”。
- 对照结论以生产实测为准；本地截图只作参考。
- 部署路径恢复正常后，把“生产版本”改回 GHCR digest，并移除本地镜像说明。
