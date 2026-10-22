# 视觉验收

前端主体使用 assistant-ui 的 Thread / Message / Composer primitives，基础控件来自 shadcn/ui（Radix Rhea）与 AI Elements，视觉规则见根目录 [`DESIGN.md`](../DESIGN.md)。视觉验收证明真实浏览器中的对话流、composer、侧栏和浮层可用；生成图只能作为参考，不能替代截图。

最后更新：2026-10-22 HKT。视觉结果只证明测试目标版本，不能据此推断生产已更新。

## 2026-10-22 接手回归

- 新增 `interaction-recovery.test.ts`：真实 Go/SQLite/WebSocket，模型仅替换为本地合成上游。覆盖上游首次500后重试、停止保留片段、双游客私聊隔离与未读、后台标题与通知、通知开关持久化、两步屏蔽/解除、真实长按及短视口草稿、真实 worker 更新与刷新操作。
- 测试目标必须为 loopback，并显式提供 `E2E_CHAT_FIXTURE_URL`。模型替身支持首次500、延迟与慢流；后台可见性和 Notification API 使用受控替身，不等于真实操作系统通知授权验收。
- `bot-run-control.test.ts` 的 1440/390 × 明暗 × 中英文矩阵按新契约验证停止片段保留，并继续拒绝50个迟到片段和迟到 final。忽略的运行事件不再发布空的状态补丁，避免密集消息触发 React 更新次数上限。
- 手势验收必须检查松手后菜单仍在，并至少存在3项可操作菜单项；只检查按住期间可见会漏掉兼容点击误触。截图禁用有限动画，避免把过渡帧作为最终视觉证据。
- 390×460 短视口下输入框和未发送草稿保持可见；这不替代 iOS/Android 真机软键盘验证。
- 私聊通知开关沿用会话偏好协议，通知正文固定不含私聊内容；缺少显式语言设置时，通知与页面使用同一个语言检测函数。
- 静态 shell 与 `/sw.js` 均为 `no-store`，新 worker 接管后提供显式刷新提示。缓存版本随本批前端更新，以便长驻页面检测到新版本。
- 最终验证：TypeScript、生产 build、后端全量测试与 vet 通过；Vitest 37文件/636项通过；两组浏览器回归16/16通过。ESLint全量0 error，49个既有warning；最后改动文件的检查无新增问题。已人工复核长按菜单、私聊通知、停止片段与窄屏输入截图。

## 设计基线

- 外壳：桌面会话栏 240px，可收起为 72px 图标轨；侧栏与主区靠底色分层、不用分隔线；桌面会话行为单行 36px（移动端 44px 触控），图标盒 20px；底部身份行打开账户菜单（登录、设置、离开）；移动端全屏，侧栏使用 Sheet。
- 对话流：`AssistantRuntimeProvider` + `useExternalStoreRuntime` 桥接 WebSocket / Zustand；`ThreadPrimitive.Root/Viewport/Messages/ViewportFooter` 负责滚动、自动贴底和空状态，每条消息由 `MessagePrimitive.Root` 包裹。日期按天插入居中标签。
- 消息：`Message`、`MessageContent`、`Bubble`、`BubbleContent`；助手消息用 `agent` 气泡，人类消息用 `secondary`，自己的消息用 `brand` 气泡右对齐且不显示名称，只保留头像、已读状态和时间。连续消息共享头像列，同侧首条与后续条目的气泡边缘必须一致。私人助手 1:1 使用 minimal 呈现：不渲染头像、名称、逐条时间与回执，正文 16px，同一会话内超过 5 分钟的间隔插入居中时间刻度；悬停操作条贴在气泡侧边并垂直居中，不覆盖正文。
- 宽度：顶栏内容、对话流和 composer 共用居中的 `max-w-3xl`（768px）列；桌面宽屏两侧留白，平板和移动端自然铺满。
- 流式状态：收到首个片段前显示与助手气泡同高的三点气泡；思考摘要与工具调用用内联 `Spinner` 表示进行中，不伪造步骤或 trace 时间线。
- 输入：`ComposerPrimitive.Root` / `Input` / `Send`，`bg-card`、`border-foreground/10`、32px 圆角、56px 最小高度、双层轻阴影，聚焦只加深边框；左侧加号菜单含表情与图片，空输入且浏览器支持语音时右侧显示麦克风，运行中发送键切换为停止；断线时整体禁用。
- 邀请：宽屏群聊右上显示 Muse 式邀请 pill；桌面复制当前房间链接并提示，触屏设备优先调用系统分享面板。窄屏入口在更多菜单，私聊不显示。
- 语音输入 E2E（注入 fake `SpeechRecognition`，避免真实麦克风授权）：空 composer 点击麦克风后文本按预期追加到输入框，输入后切回发送键；真实语音识别仍取决于浏览器和麦克风权限。
- 公共房间：进入即消息 timeline；空房间只显示轻量状态，composer 固定在滚动区域外的底部，不出现 AI 欢迎页或建议胶囊。
- 私人助手空状态：居中的 `ThreadWelcome` + composer + `SuggestionPrimitive` 建议胶囊，点击建议直接发送；有历史时显示对话流。
- 动效：只使用 `tw-animate-css` 的 `animate-in` / `fade-in` / `slide-in` / `zoom-in` 与 Radix Sheet/Dialog 的 `data-open` / `data-closed` 动画；生成中的三点呼吸只表示进行中，不做装饰性循环动画。
- 浮层：设置、线程和认证分别使用 Sheet、Sheet、Dialog；搜索使用 Command。
- 品牌：产品间统一字体、Logo 规则与基础组件，当前字体为 Geist / Geist Mono 与中文系统字体回退；各产品保留既有品牌强调色，颜色不要求统一。
- 颜色：中性色表面为底，产品强调色经语义 token 使用；状态色表达状态，不用渐变、光晕或毛玻璃装饰。
- Account 登录：使用品牌契约指定的 TokenDance 标记与完整提供方名称；标记轮廓、来源及 SHA-256 见 `frontend/public/brand/manifest.json`。标记不得替代可访问文案，明暗主题与窄屏都要可读。

## 验收命令

从仓库根目录检查并构建：

```bash
cd frontend
pnpm exec tsc --noEmit
pnpm test
pnpm run build
```

另一个终端从仓库根目录启动本地验收目标，数据库使用忽略目录，不复用生产数据：

```bash
mkdir -p .tmp/visual
cd backend
CHAT_DB_PATH="$(pwd)/../.tmp/visual/chat.db" \
CHAT_FRONTEND_DIR="$(pwd)/../frontend/dist" \
CHAT_ADDR=127.0.0.1:8091 \
CHAT_OIDC_ENABLED=false \
go run .
```

从仓库根目录运行浏览器验收：

```bash
cd frontend
VISUAL_BASE_URL=http://127.0.0.1:8091 \
VISUAL_OUTPUT_DIR="$(pwd)/artifacts/visual" \
pnpm run visual:acceptance

E2E_BASE_URL=http://127.0.0.1:8091 \
pnpm exec playwright test src/e2e/account-login.test.ts
```

Account 登录 CTA 测试在浏览器中模拟配置和 session，用来验证入口、样式与可访问性；它不替代真实 Account OIDC、邮箱验证或退出流程的集成验收。13 场景脚本会发送演示消息；需要 LLM 的场景使用专用测试 provider，不把浏览器 mock 当成真实模型完成证据。

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
| 1440x900 / 390x844 / 320x844 | light / dark，中英 | Account 登录完整文案、Logo 资源、默认/hover/focus 对比度与无溢出 |
| 1440x900 / 390x844 | light / dark | 私聊线程发送、公共/私聊切换、迟到线程结果不会覆盖当前视图 |

硬门槛：

- 无水平滚动、无控制台错误、无旧 IM 文案；
- composer 为 28px 胶囊（24–32px），高度 48-140px，移动端不超过视口 25%，桌面宽度至少 720px；
- 每条可见消息有表面标记；气泡（非 assistant 纯文本）宽度不超过对话流的 0.88，公共房间同时出现自己和对方消息；
- 同一视口内所有接收消息的气泡左缘偏差不超过 1px；
- 连续的自己消息气泡右缘偏差不超过 1px；
- 断线时 composer 输入与发送均不可用；
- 移动端标题容器至少 120px 宽且未被裁剪；
- 设置抽屉桌面宽 384px，移动端宽度在 320-420px 内并完整贴合视口；
- 移动侧栏与设置抽屉内没有低于 44x44 的可视控件。
- 页面 CSS 与组件样式中没有 `gradient` / `backdrop-blur` 装饰。
- Account 登录文字默认、hover、focus 对比度至少 4.5:1；点击区域至少 44px 高，320px 视口文案可换行且 Logo 不压缩；标记资源能实际解码，不只检查 URL 非空。
- 线程回复携带当前会话收件人；公开与私聊线程结果不会互相覆盖，关闭/切换后迟到返回不能重新打开或覆盖另一个线程。
- Account 退出的浏览器验收还必须确认：POST 携带派生的 `X-CSRF-Token`，
  经 `/api/logout/account/callback` 校验并消费同浏览器 state 后回跳到
  `/?oidc_logged_out=1`，展示成功提示、清掉 marker URL，并恢复游客态。

## 2026-10-20 验收

- 输出目录：`%TEMP%\tdchat-aui-acceptance-final`
- 环境：本机 Go 后端托管最新 `frontend/dist`，临时干净 SQLite，不涉及生产。
- 结果：13 个场景全部通过，`oldLabels=0`；桌面阅读列 896px、composer `848x94`，移动 composer `358x94`，圆角 14.4px；桌面设置 384px、移动设置 378px。
- 接收消息左缘偏差 0px，自己消息右缘偏差 0px，名称与气泡右缘偏差 0px；连续消息首条与后续条目保持同一条边缘。
- 已人工查看桌面明暗、移动明暗、TokenBot 空状态、设置与认证截图；欢迎区、composer 和建议胶囊作为一组居中，有消息时对话流贴底，自己的消息头像、回执与气泡形成同一右侧列。

## 2026-10-21 本轮验收状态

- Account 登录反差和窄屏溢出修复已通过 12 组本地浏览器场景：1440/390/320px × 明暗主题 × 中英文，覆盖文字可读性和触控/布局边界。
- 包含 Account 小 Logo 与侧栏暗色去反相修复的最终登录 E2E 12/12、视觉 13/13 全部通过；品牌 agent 已人工查看桌面明暗与 320px 明暗截图，侧栏保持原品牌蓝。线程发送/切换/迟到响应回归 14/14 通过。
- 前端完整验证：TypeScript、build 通过；Vitest 29 文件、542 测试通过；lint 0 error，47 个既有 warning。以上不等于完整真实 OIDC 往返流程验证。
- 本地证据目录：`/srv/dev/artifacts/chat-login-takeover-20261021/final-with-brand/`，包含 `login-e2e-final.log`、`visual-final/metrics.json` 和 `brand-review.md`。
- 后端公开内容过滤、引用/线程会话授权和未知 `to` 拒绝已完成全量 race、vet、build 验证；这些安全测试不能替代浏览器视觉验收。
- 本轮为已验证、尚未部署的本地增量；旧的 2026-10-20 截图指标仅保留历史基线，不能证明本轮界面或生产已更新。

## 2026-10-21 交互与时间线打磨

- 本地 mock 流式链路：乐观消息 19–48ms 可见；首个增量 112–209ms；完整回复 1.28–1.41s；无 long task（>50ms）；同一回复只渲染一次。
- 失败路径：上游返回 500 时草稿移除，并追加一条居中提示「这次回复没有完成，请重试。」；控制台无错误。
- 发送拒绝：服务端错误回传 `client_message_id`，前端标记对应乐观消息并提供「重试」，不再静默假装送达。
- 重连历史：join 快照按时间序合并；`load_history` 分页单独标记，不再把重连快照前插到旧窗口之上。
- 视觉验收 13/13 通过；桌面 composer 848x94、圆角 14.4px，私聊阅读列 896px，接收/自己消息边缘偏差 0px。
- 本机证据目录：`.tmp/visual/final-out/`（截图 + `metrics.json`）、`.tmp/visual/stream-*.png`；`.tmp/` 不入 Git。
- 以上为本地验证，不代表生产已部署。

## 2026-10-22 Muse 对照、主动参与与图片消息

- 视觉验收 13/13 通过（干净 SQLite，本地 Go 后端托管最新 dist）：composer `720x56`、圆角 `32px`；桌面三栏为 72px 图标轨 / 240px 会话栏 / 768px 阅读列 / 360px 信息栏；移动 composer `358x56`；设置抽屉 384px（桌面）/378px（移动）；无水平溢出、无旧 IM 标签、无 console/page error。
- 气泡按实测 Muse token 对齐：自己的消息浅色 `#B8DBFE` + `#111112`（深色 `#0074E8` + 白字），助手 `#E9EAEB`（深色 `#1F1F20`）；22px 圆角、11px 15px 内边距、16px 正文；分组仅贴合边收 6px、组间距 4px。
- Hover 操作按 Muse 改为气泡侧边居中的 26px 圆形按钮，opacity + scale 150ms；不再使用气泡下方常驻容器。
- 群聊与私聊右侧信息栏分化：顶部为身份卡，下面三段 tab——群聊为成员/动态/助手，私聊为动态/来源/对话；群聊列出在线人类成员，私聊只列当前账号的 run 和最近引用，切换模式时回退到该模式的首个 tab。
- 本批改版已在生产 `d186ae9`（生产主机本地构建镜像，GitHub Actions 账单阻塞期间的临时发布路径）上用真实浏览器实测：1440×900 下阅读区无标题/状态横条，仅右上浮动控制组；邀请 pill 可复制链接并提示；空 composer 显示麦克风，输入文字后切回发送；群聊/私聊信息栏（图标 tab、按天活动、居中身份卡、对话统计）、回复 chip、折叠图标轨、设置抽屉与浅深主题切换均正常；1300×900 下信息栏与切换按钮隐藏、阅读列恢复 768px；390×844 下信息栏隐藏、侧栏走 Sheet、移动 composer `358x56`，无水平溢出和组件重叠。
- 刷新后（游客无法加载私聊历史）活动行仍由服务端 `trigger_preview` 补齐摘要，实测三条运行均有描述；群聊成员列表首位显示 TokenBot（AI），随后是真人成员。
- 三栏信息栏从 ≥1368px 启用，1280–1367px 保留 768px 阅读列；宽屏阅读区不再渲染标题/状态横条，只保留右上浮动控制组。引用回复在 composer 上方显示可取消的引用 chip，切换会话会清除；长按消息打开完整菜单，未启用选择模式时不显示无效的「选择」项；已读回执弹层改为 fixed 定位，不再被消息行裁切；平滑滚动尊重 `prefers-reduced-motion`。
- Bot 多段回复：模型可在最终答案前调用 `send_message` 发布过渡消息（群聊 1 条、私聊 2 条、≤600 字符），过渡消息作为普通 Bot 消息落库并以相同会话范围投递，不占用唯一的 `run_id` final；私聊工具卡片隐藏该调用，只保留消息本身。生产实测两次私聊 run 均 `tool_calls=1`，数据库分别出现「我先整理一下」「收到」两条 `run_id` 为空的过渡消息，final 仍各自唯一。单连接复测中，客户端把过渡消息渲染为独立气泡（「收到 E2E」），随后再渲染 final 气泡。
- 移动端加号、发送/停止和欢迎建议胶囊均为 44px 触控目标，桌面回落到 32px；桌面端靠 hover/focus 才显示的消息操作条在移动端不渲染。
- 暗色画布按 Muse 实测提到 `oklch(0.19)`（`#181819`），card / popover / sidebar 同步上移一档，助手气泡继续用 `#1F1F20`，避免纯黑画布的高对比。
- 信息栏活动行按 Muse 实测对齐：40px 图标块（10px 圆角）、8px 行内边距、14/20 标题、13/18 描述、12/16 等宽时间；日期标题 16/22、顶部 8px。tab 高亮改为 200ms 滑片，内容切换 150ms fade。
- 图片消息实测：composer 加号菜单 → png 上传 → `/uploads/images/<uuid>.png` → 消息内 `<img>` 成功解码（naturalWidth>0）。服务端解码并重编码去除元数据；登录 5MB / 游客 2MB，按 IP 限流。
- 主动参与实测：`CHAT_BOT_AMBIENT=true` 时，无 @ 的 Go 技术问题被 selector 判定为 reply（约 0.3s），3.3s 内公开发布回复；显式 @ 路径保持不变，日志只记录动作与理由摘要。
- 私聊端到端实测（生产 `6d1a33a`）：发送问题后出现三点气泡、「正在回复」栏和可切换的停止键，完成后气泡内保留折叠的思考过程；发送新闻搜索请求触发 `web_search`，回答内列出 6 条来源并区分「仅发现 · 未读取」「已读取 · 已引用」，信息栏当天分组同步出现两条运行记录。
- 连接稳定性：修复客户端被新登录踢下线时的 `send on closed channel` panic（改用关闭信号通道）；重连前两次静默、2s/4s 指数退避，界面连接态延迟 1.5s 显示。
- 验证：Vitest 37 文件 617 测试、Go 全量测试与 vet、TypeScript、生产 build 通过；E2E 子集 45 项通过、9 项按环境跳过；真实模型文本造成的旧标签误伤已改为只检查 UI chrome。
- 证据：`.tmp/visual/final-20261022/`（截图 + `metrics.json`）；`.tmp/` 不入 Git。
- 以上为本地验证，生产部署与线上验收另行记录。

## 2026-10-22 输入、私聊未读与终态回复

- IME：中文输入法组合期间的 Enter 不再误触发 `@` 候选选择；`isImeComposing` 同时识别 `isComposing` 与 `keyCode=229`。
- 移动端助手气泡宽度改为 `max-w-[85%]`，桌面保留 `min(85%, calc(100%-148px))` 的 hover 操作空间，窄屏长回答不再压到屏幕边缘。
- 屏蔽入口打通：消息上下文菜单两步确认后发送 `block` 并写入本地屏蔽列表；设置抽屉列出已屏蔽用户并可解除。服务端原有屏蔽广播不变。
- 私聊未读：未查看该助手的回复时累计会话徽标与标题计数，进入会话或回到前台即清除；系统通知只显示「{助手名} 有新回复」，不携带私聊正文。
- 终态回复：停止后保留已流出的片段并标记「已停止生成」，可复制或重试原请求；没有片段时追加一行停止提示。失败仍清空草稿并显示一次性提示，提示上的「重试」会重新发送原请求。
- 验证：TypeScript 通过；受影响 Vitest 140 项通过（全量耗时见部署记录）；以上为本地验证，生产部署与线上截图另行记录。
