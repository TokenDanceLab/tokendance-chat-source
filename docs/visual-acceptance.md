# 视觉验收

前端主体使用 assistant-ui 的 Thread / Message / Composer primitives，基础控件来自 shadcn/ui（Radix Rhea）与 AI Elements，视觉规则见根目录 [`DESIGN.md`](../DESIGN.md)。视觉验收证明真实浏览器中的对话流、composer、侧栏和浮层可用；生成图只能作为参考，不能替代截图。

## 设计基线

- 外壳：桌面侧栏 272px，侧栏与主区靠底色分层、不用分隔线；侧栏内容统一 16px 内缩，图标盒 28px，标题统一从 54px 起；头部横跨主区，标题与消息名称对齐；底部身份行打开账户菜单（登录、设置、离开）；移动端全屏，侧栏使用 Sheet。
- 对话流：`AssistantRuntimeProvider` + `useExternalStoreRuntime` 桥接 WebSocket / Zustand；`ThreadPrimitive.Root/Viewport/Messages/ViewportFooter` 负责滚动、自动贴底和空状态，每条消息由 `MessagePrimitive.Root` 包裹。日期按天插入居中标签。
- 消息：`Message`、`MessageContent`、`Bubble`、`BubbleContent`；助手消息用 `ghost` 纯文本，人类消息用 `secondary` 气泡，自己的消息右对齐且不显示名称，只保留头像、已读状态和时间。连续消息共享头像列，同侧首条与后续条目的气泡边缘必须一致。
- 宽度：顶栏内容、对话流和 composer 共用居中的 `max-w-4xl`（896px）列；桌面宽屏两侧留白，平板和移动端自然铺满。
- 流式状态：消息行内只显示 `Spinner` 和状态文本，不伪造步骤或 trace 时间线。
- 输入：`ComposerPrimitive.Root` / `Input` / `Send`，`bg-muted/30`、`border-foreground/10`、16px 圆角、8px 内边距，无阴影，聚焦只加深边框；断线时整体禁用；高度 56–140px，左右与头部操作区对齐。
- 公共房间：进入即消息 timeline；空房间只显示轻量状态，composer 固定在滚动区域外的底部，不出现 AI 欢迎页或建议胶囊。
- 私人助手空状态：居中的 `ThreadWelcome` + composer + `SuggestionPrimitive` 建议胶囊，点击建议直接发送；有历史时显示对话流。
- 动效：只使用 `tw-animate-css` 的 `animate-in` / `fade-in` / `slide-in` / `zoom-in` 与 Radix Sheet/Dialog 的 `data-open` / `data-closed` 动画；不用装饰性脉冲或常驻动画。
- 浮层：设置、线程和认证分别使用 Sheet、Sheet、Dialog；搜索使用 Command。
- 颜色：单色墨阶，无渐变、光晕或毛玻璃；颜色只表达状态与身份。

## 验收命令

```powershell
cd frontend
pnpm exec tsc --noEmit
pnpm test
pnpm run build

cd ..\backend
$env:CHAT_DB_PATH = Join-Path $env:TEMP 'tdchat-visual.db'
$env:CHAT_FRONTEND_DIR = (Resolve-Path ..\frontend\dist).Path
$env:CHAT_ADDR = ':8091'
go run .

cd ..\frontend
$env:VISUAL_BASE_URL = 'http://127.0.0.1:8091'
pnpm run visual:acceptance
```

脚本默认只接受本机目标，因为它会写入演示消息。输出目录包含截图和 `metrics.json`。

## 必测场景

| 视口 | 主题 | 场景 |
|---|---|---|
| 1440x900 | light / dark | 公共聊天、自消息、对方消息、AI card 回复 |
| 1440x900 | light | TokenBot 私聊空状态与 assistant context |
| 768x1024 | light | 平板聊天宽度 |
| 390x844 | light / dark | 移动聊天、标题、触控目标 |
| 390x844 | light | 移动侧栏、移动设置抽屉 |
| 1440x900 | light | 设置抽屉 |
| 1440x900 / 390x844 | light | Account 菜单中“退出 Account”与“离开聊天”并列可见 |

硬门槛：

- 无水平滚动、无控制台错误、无旧 IM 文案；
- composer 圆角 12–20px，高度 56-140px，移动端不超过视口 25%，桌面宽度至少 720px；
- 每条可见消息有表面标记；气泡（非 assistant 纯文本）宽度不超过对话流的 0.88，公共房间同时出现自己和对方消息；
- 同一视口内所有接收消息的气泡左缘偏差不超过 1px；
- 连续的自己消息气泡右缘偏差不超过 1px；
- 断线时 composer 输入与发送均不可用；
- 移动端标题容器至少 120px 宽且未被裁剪；
- 设置抽屉桌面宽 384px，移动端宽度在 320-420px 内并完整贴合视口；
- 移动侧栏与设置抽屉内没有低于 44x44 的可视控件。
- 页面 CSS 与组件样式中没有 `gradient` / `backdrop-blur` 装饰。
- Account 退出的浏览器验收还必须确认：POST 携带派生的 `X-CSRF-Token`，
  经 `/api/logout/account/callback` 校验并消费同浏览器 state 后回跳到
  `/?oidc_logged_out=1`，展示成功提示、清掉 marker URL，并恢复游客态。

## 2026-10-20 验收

- 输出目录：`%TEMP%\tdchat-aui-acceptance-final`
- 环境：本机 Go 后端托管最新 `frontend/dist`，临时干净 SQLite，不涉及生产。
- 结果：13 个场景全部通过，`oldLabels=0`；桌面阅读列 896px、composer `848x94`，移动 composer `358x94`，圆角 14.4px；桌面设置 384px、移动设置 378px。
- 接收消息左缘偏差 0px，自己消息右缘偏差 0px，名称与气泡右缘偏差 0px；连续消息首条与后续条目保持同一条边缘。
- 已人工查看桌面明暗、移动明暗、TokenBot 空状态、设置与认证截图；欢迎区、composer 和建议胶囊作为一组居中，有消息时对话流贴底，自己的消息头像、回执与气泡形成同一右侧列。
