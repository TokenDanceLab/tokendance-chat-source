# 前端设计基线

最后更新：2026-10-21

本文件约束 TokenDanceChat 前端的外观和组合方式，是视觉评审与改动的入口。上游依据：

- Vercel 公开的 [`design.md`](https://vercel.com/design.md)
- shadcn/ui 的 [AI Elements](https://ui.shadcn.com/docs/ai-elements) 与 Chat 组合
- [assistant-ui](https://www.assistant-ui.com/) 的 Thread / Message / Composer primitives

冲突时，以 Vercel `design.md` 的克制原则为准；对话流与输入区 API 以 assistant-ui primitives 为准，基础控件以 shadcn registry 为准。

## 视觉原则

- 以中性墨阶为主体，保留既有产品主题色和家族品牌蓝的克制点缀；状态（在线、错误、警告）与用户身份用色保持语义明确，不扩张为彩色面板或装饰氛围。
- 禁止渐变、光晕、毛玻璃、纹理、彩色侧条和装饰性阴影。浮层之外不加 elevation。
- 字体消费[家族 Typography 契约](https://github.com/TokenDanceLab/tokendance-design/blob/master/docs/design/design-system.md#2-product-feel)的自托管 Geist / Geist Mono 与 CJK fallback。Chat 本地字号：消息正文 16px，输入区 15px，标题 14px，名称与行标题 13px（`text-name`），说明与时间 12px（`text-xs`），标签 11px（`text-label`）；10px 只用于紧凑徽标与头像首字母（`text-micro`）。标题用句式大小写，不用全大写小标题。
- 圆角克制：控件 6–12px，输入区 32px 胶囊，气泡 22px。不要把所有元信息做成胶囊。
- 边界优先用底色分层、留白和对齐表达；只有滚动内容需要被截断时才加 1px 顶线，不为每一段内容套卡片，不嵌套面板。
- 动效只解释状态变化。进入/退出统一用 `tw-animate-css`（`animate-in` / `animate-out` + `fade` / `zoom` / `slide`），浮层用 Radix 的 data-state 动画；只保留手势拖拽、投票进度和图片缩放这三类功能性过渡，不写关键帧、不做装饰性缩放。
- 对话流、顶栏内容和 composer 共用一条居中的 768px 阅读列，宽屏留白、窄屏自然铺满。对方与助手消息左对齐，自己的消息右对齐并与头像列镜像；信息密度参考 WeChat，品牌色、整行高亮和红色角标不参考。
- 宽屏（≥1368px）采用三栏：左侧会话栏（240px，可收起为 72px 图标轨）、中间阅读列、右侧 360px 信息栏。信息栏顶部是身份卡，下面是三段 tab：群聊为成员 / 动态 / 助手，私聊为动态 / 来源 / 对话。活动按天分组，行高由 40px 图标块、14px 标题和 13px 描述组成；tab 高亮用 200ms 滑片，并尊重 `prefers-reduced-motion`。1280–1367px 只保留会话栏与阅读列，避免 768px 阅读列被三栏挤压；更窄时信息栏收起，侧栏用 Sheet。

## 品牌资产映射

品牌范围以 [`tokendance-design` 的品牌契约](https://github.com/TokenDanceLab/tokendance-design/blob/master/docs/design/design-system.md#3-brand-assets) 为准。Chat 的导航、安装图标和页面标题表达 Chat 产品身份；登录按钮表达身份提供方 **TokenDance Account**，不可拿 Chat 名称或 AgentHub 图标代替。

`OidcLoginButton` 使用 `public/brand/tokendance-mark-white-transparent.svg` 的原始三条柱形轮廓，来源和 SHA-256 见同目录 `manifest.json`。以 `currentColor` alpha mask 呈现，随按钮语义前景切换颜色；不改 SVG 路径、不附加底板或渐变。图形在 18px 槽中按 18px 光学高度显示，与完整登录文案间隔 8px。图形为 `aria-hidden` 装饰，链接的可访问名称只读一次完整文案；窄屏允许文字换行，图形不压缩，触控高度至少 44px。

侧栏使用原始蓝色家族 mark，深色主题也保持原色，不用 `invert` 滤镜把品牌蓝变为橙色。

## 组合映射

| 界面 | 组件 |
|---|---|
| 对话流骨架 | `AssistantRuntimeProvider` + `useExternalStoreRuntime` + `ThreadPrimitive.Root` / `Viewport` / `Messages` / `ViewportFooter` |
| 单条消息 | `MessagePrimitive.Root` 包裹 AI Elements `Message` / `MessageAvatar` / `MessageHeader` / `MessageContent` / `Bubble` / `BubbleContent` / `MessageFooter` |
| 日期分隔 | 消息行内的居中日期标签，按天变化 |
| 流式状态 | 收到首个片段前显示三点气泡；工具调用与思考摘要用内联 `Spinner` 表示进行中 |
| 输入区 | `ComposerPrimitive.Root` / `Input` / `Send`；提词弹层和 emoji 只做输入增强 |
| 空状态与建议 | 居中的 `ThreadWelcome` + `ThreadPrimitive.ViewportFooter`（composer + `SuggestionPrimitive` 胶囊） |
| 侧栏 | 品牌区 + `ScrollArea` + 纯文本菜单项；移动端用 `Sheet` |
| 账户与设置 | 侧栏底部身份行 + `DropdownMenu`（登录 / 设置 / 离开） |
| 浮层 | `Sheet` / `Dialog` / `DropdownMenu` / `Command` |

## 使用规则

- 助手消息用 `Bubble variant="agent"`（浅色 `#e9eaeb` / 深色 `#1f1f20`）气泡；其他人类消息用 `secondary`；自己的消息用 `brand` 气泡（浅色 `#b8dbfe` 深字 / 深色 `#0074e8` 白字）右对齐。气泡圆角 22px、内边距 11px 15px、正文 16px，参考 Muse 的对话表面。
- 单条消息的 hover 操作（引用、表情、复制、更多）贴在同一行的气泡侧边并垂直居中，默认透明并轻微缩小，hover/focus 时淡入；不额外增加常驻工具条。
- 同一发送者 2 分钟内的连续消息合并为一组：只显示一次头像与名称，消息间距 2px，气泡圆角按首/中/尾连续过渡；发送回执只出现在最后一条。
- 对方与助手消息左对齐，自己的消息右对齐并与右侧头像列镜像。自己的消息不显示名称，只保留头像、方向、回执和时间，避免「你」挤压气泡边缘。连续消息只保留一个头像和名称，首条与后续条目保持同一条气泡边缘；对话流、顶栏和 composer 共用同一条 768px 阅读列与同一水平内边距。
- 消息操作放在名称行或气泡上方的悬浮条，默认隐藏，桌面 hover/focus 显示；分组后续消息只显示单个“更多”按钮，避免重复工具条；移动端用长按菜单和滑动手势，不常驻工具条。
- 账户级操作只在左下身份菜单，顶栏只放内容级操作（搜索、更多）；不把登录、设置、离开散落在多处。
- 流式生成只显示一个内联 `Spinner`/三点气泡和状态文本，不编造步骤或时间线；动效统一尊重 `prefers-reduced-motion`。
- 对话流只保留一套状态：WebSocket / Zustand 是消息真相源，`useExternalStoreRuntime` 只做只读映射和发送桥接，不复制消息仓库。
- 断线时 composer 整体禁用；不在未连接时接受输入或静默丢弃发送。
- 输入区使用 `ComposerPrimitive` 表面：`bg-muted/30`、`border-foreground/10`、28px 胶囊圆角、单行布局（左侧表情、中间输入、右侧发送/停止），无阴影；聚焦只加深边框，不加光晕。发送按钮 36px 圆形，运行中切换为停止；模型上下文在宽屏以紧凑 chip 显示，窄屏隐藏。
- 新增视觉只允许从 `src/components/ui`、`src/components/ai-elements` 与 assistant-ui primitives 组合，不引入平行设计系统。

## 验收

视觉验收流程与硬门槛见 [`docs/visual-acceptance.md`](./docs/visual-acceptance.md)。改动后必须在真实浏览器检查明暗主题、桌面与移动断点，并确认没有水平溢出、控制台错误或装饰性渐变。

## assistant-ui 边界

- 采用 assistant-ui 官方 primitives，并复刻官方 `Thread` 的布局、间距、空状态、滚动和 composer 结构；不整块安装 registry 中的附件、语音、分支、tool 与 reasoning 模块，避免把未使用能力带进产品。
- `ChatThread` 负责 runtime 桥接、滚动、空状态和 composer；`MessageBubble` 只做消息呈现，沿用 AI Elements 的 `Message` / `Bubble` 结构承载头像、分组、回复、投票和反应。
- 后续接入 tool / data part 时，优先用 assistant-ui 的 tool UI / generative UI 组合，不另写一套消息协议。
