# TokenBot Agent 架构

最后更新：2026-10-23　适用代码：`backend/hub`、`backend/llm`、`backend/store`、`backend/websearch`　Prompt：`2026-10-23.2`

这份文档描述**当前工作树代码行为**、取舍依据和升级条件。2026-10-21 的独立 DeepSeek Search/Fetch、来源引用与工具预算已实现，Chat 生产发布仍以验证/发布记录为准。第 7 节区分当前 provider 方案与后续规格，研究材料不代表功能已经发布。

## 结论

| 问题 | 决定 |
|---|---|
| 群聊还是单聊 | 同一个 Bot，两种会话：公共房间显式 `@`，私聊直接对话 |
| 群里什么时候说话 | 被点名（`@` 或回复 Bot 的消息）必然说；`CHAT_BOT_AMBIENT` 开启后普通消息经无工具判断器受控接话，默认关闭 |
| 记忆 | SQLite transcript 是唯一真相源；近期窗口 + FTS 检索，同一份数据两种读法 |
| 要不要记忆框架 | 不要。现在的问题是"找回原话"，不是"跨会话画像" |
| Prompt | 每个模式逐字节静态，带版本号和 SHA；易变信息只放在 user turn 里 |
| 缓存 | 对话窗口用锚点追加，不逐条滑动；只有溢出时才付一次 cache miss |
| 工具 | 基础工具为时间 + 本会话搜索；按配置增加独立 DeepSeek Search/Fetch，无 shell、文件、任意 SQL |
| 任务 | 按会话排队；冻结 trigger 与输入；持久预算；可取消；每 run 一条幂等 final，可在 final 前发有界 interim 消息 |
| 主动参与与多 Bot | 主动参与已实现（第 7.2 节，flag 默认关）：无工具判断器 + 每房间冷却/配额 + 失败退避；多 Bot、追加指令与多消息发布仍属后续规格 |

## 1. 调研结论

### Grok Bot：自动选人、显式定向与单一 owner

2026-10-21 重新核验官方文档后，群聊合同如下：

- 普通群消息可由参与的 Bots 决定谁回复；`@Bot` 和回复某条 Bot 消息是显式定向，不是唯一触发方式。
- 每个阶段只保留一个 owner，handoff 本身需要可见时才放进群聊。
- Bot 可以异步向另一个 Bot 交接；交接不要求用户不断充当中间人。
- 群聊中不展示 secret/login 请求和外部发送审批草稿；能力授权仍需独立检查。

当前 TokenBot 的显式触发是本项目的既有实现取舍，不能据此声称 Grok Bot 也只能被点名。
我们借鉴共享房间与私聊的可见性分离；下一阶段的自动选人、多消息和交接需要独立实现
调度、预算和授权，不能仅修改 prompt。当前仍是单 Bot，私聊内容不回流到房间。

参考：[Grok 原生群聊](https://docs.x.ai/grok-bot/chat-and-collaboration)、[Bot 职责与复制](https://docs.x.ai/grok-bot/bots)、[Team Bots](https://docs.x.ai/grok-bot/team-bots)。Slack 顶层消息的 @ 规则与原生群聊自动选人不是同一合同；共享 Bot 配置也不等于共享所有人的私聊。

[Grok 官方设计文章](https://x.ai/news/designing-grok-bot)（2026-09-03）进一步解释持续 Bot 身份、活动状态、按需展开当前动作和结构化产物。TokenDance 先用真实 run 状态与短活动说明体现进度，避免把内部执行记录逐条变成聊天气泡；不会先展示尚不存在的审批、后台计划或专家状态。这些页面证明公开产品合同，不证明其私有 selector、调度算法或取消计费实现。

### Dots 与 Muse：持续工作、少打扰和独立授权

- [Dots tasks and memory](https://learn.chatgpt.com/docs/dots/tasks-and-memory) 区分任务上下文、持久笔记和跨渠道披露权限；主动研究本身只读，不直接向外发消息或修改应用。
- [Muse 产品设计](https://introducing.muse.ai) 支持长对话中打断、并行任务和后台跟进；只有有实质新信息或需要输入才通知，活动记录与聊天消息分开。
- [Muse 安全设计](https://security.muse.ai) 把执行环境、凭据和动作授权分开。借鉴边界分工，不把其私有实现或安全保证当成本项目已有能力。

当前落实为：SQLite transcript 是历史真相源；公开检索固定房间，私聊检索固定本人和 Bot；公共输出在服务端过滤原始推理/工具数据。网页工具已纳入 run 授权、来源范围和独立金额预算；后续主动参与与新工具仍须逐项实现服务端边界。

### 保留现有存储与缓存

近期窗口和 FTS 检索均来自同一份 SQLite 数据，不引入第二套会话存储。system prompt、工具定义和工具顺序保持稳定，时间、用户与结果放在后续消息中；窗口通过锚点追加，只有溢出时重新锚定。缓存效果看实际 token 日志，不假设任何供应商必然命中。

工具集按实际配置注册：每个会话都有时间和该会话搜索，启用网页研究后再加 `web_search` 与 `web_fetch`；同一配置下定义与顺序保持稳定。

## 2. 交互模型

| 场景 | 触发方式 | 上下文 | 回复可见性 |
|---|---|---|---|
| 公共房间 | `@TokenBot`，或回复 TokenBot 的消息 | 本房间 transcript | 广播给房间 |
| 私聊 | 向 TokenBot 发送消息 | 本人与 TokenBot 的 transcript | 只回给本人 |

当前公共房间默认只在被点名时回复。配置 `CHAT_BOT_AMBIENT=true` 后，未被点名的普通公共消息会触发一次无工具的轻量判断（见第 7.2 节），判断是否接话；判断本身计一次模型调用并受每房间冷却、每小时配额与失败退避约束。未知 `to` 返回 `INVALID_RECIPIENT`，不会把预期私聊的内容改发公开房间。引用和线程父消息必须属于当前可见会话，回复作者和预览由数据库真实消息派生。

### 调度、输入和停止

- `backend/hub/runs.go` 按公共 `room:<room_id>` / 私聊 `dm:<actor_id>` 仲裁；每会话至多一个活跃 run，各房间之间共享全局计算槽。会话轮转并优先选与上次不同的 actor，防止同一人连续占满队列；这不是严格的跨用户加权公平调度。
- 当前等待上限为实例 64、会话 8、actor 2；全局默认同时执行 4，配置上限被限制到 16。拒绝明确返回 `RUN_QUEUE_FULL` 或预算错误，不静默丢弃已接受的请求。
- 入队前以 trigger 的 SQLite `rowid` 为上界取得独立输入副本，并保存 `input_json`；同毫秒或后来到达的消息不会替换任务。工具中的历史搜索仍是有会话边界的实时读取，不是整份数据库快照。
- `actor_id` 来自认证会话：OIDC 用 Account sub，local/guest 使用不同前缀；不接受 WS 载荷自行声明主体。当前只将 actor 贯穿 run 与关联消息，并未完成所有历史消息/会话主键的 principal 迁移。
- 服务端发送 `bot_run` 状态：`queued → running → completed | failed | cancelled`。客户端用 `bot_cancel{run_id}` 取消自己的任务，跨标签页同一 actor 可操作；未知、不可访问任务使用统一错误。追加指令尚未实现，后续被定向给 Bot 的消息目前作为独立 run 入队。
- 取消传播 context，并在模型事件、工具执行前后及发布时检查 run 与认证状态。取消与发送串行化；迟到结果不再发布。运行中的槽位在实际执行退出后释放，避免只改 UI 状态就释放仍占用的计算资源。
- `run_id` 同时作为 stream 关联；预分配 `message_id` 对应最终气泡。数据库事务提交授权检查、预算结算、terminal 状态和唯一 final，防止重复交付。
- 从入队起最多 2 分钟，实际执行最多 60 秒。重启将未结束 run 标为 `failed/interrupted`，不自动重新调用模型；这是持久记录与明确中断，不是断点自动续跑。

## 3. 记忆

分三层，都是同一份数据的不同读法，没有第二套真相源：

1. **工作记忆**：发给模型的近期 transcript。默认窗口 20 条（`CHAT_LLM_MEMORY_SIZE`），
   实际请求会在 20–40 条之间追加增长，溢出后压缩回 20 条。
2. **长期记录**：`messages` 表本身。用户能翻到的历史，就是 Bot 有资格检索的范围。
3. **检索**：SQLite FTS5。
   - `search_public_messages`：强制当前房间、公开消息。
   - `search_private_messages`：强制 `room_id = ''`，只匹配本人与 Bot 两个方向。

### 为什么窗口要带锚点

假设每轮都取"最近 20 条"。第 21 条消息到达时，第一条被挤掉，整个消息前缀都变了，
provider 只能命中 system + tools 的缓存，正文全部重新计费。

现在的做法是记住本轮窗口最老的一条消息（锚点）：

- 新消息到达时只追加，前缀不变，缓存继续命中。
- 锚点掉出 40 条的回看范围时才重新锚定到最近 20 条，接受一次 cache miss。
- 锚点只存在进程内；重启后重新锚定一次，不影响正确性。

代价是上下文从固定 20 条变成 20–40 条；换来的是多轮之间稳定命中。真实命中率看
`cache_hit` 日志，不靠猜。

### 不变量

- 私聊消息不进入房间上下文，也不进入公共检索。
- 其他用户的私聊永远不可见。
- 已删除消息不进入窗口和检索。
- 私聊中同一个 Bot 对不同用户使用独立历史；公共房间中的成员则共享该房间公开历史。
- Bot 的名字和全部触发别名不可注册、不可加入：否则既能冒充 AI，也能把冒充者的话
  以 assistant 身份写进后续上下文。

### 何时才升级

- **摘要**：窗口溢出频繁，且日志显示检索没有被调用。摘要必须能指回原文。
- **向量检索**：问题从"找回原话"变成"按意思找"，且 FTS 命中率数据不达标。
- **用户画像**：出现跨会话的稳定偏好，且用户明确要求记住。
- **共享记忆**：出现"团队级知识"需求，并且有明确的可见性规则。

## 4. Prompt 与缓存

- 唯一实现在 `backend/hub/prompt.go`。
- 每个模式生成一次，逐字节静态。
- `TokenBotPromptVersion` 与 `PromptSHA` 标识两个模式的 prompt；当前版本与指纹进入日志，`/api/config` 只返回当前 handler 实际暴露的字段。
- 回复长度是 prompt 级合同（`## Length` 段，按模式静态分叉）：群聊默认 1–3 句、寒暄只回几个字；私聊默认一两段、按需展开。长度不做后处理截断——硬截断会切断句子和代码围栏，成本上界已由 MaxTokens 与每日预算约束。
- 每轮日志带 `prompt`、`prompt_sha`、`cache_hit`，用于发现"没改代码但前缀变了"。

每轮日志：

```
bot reply mode=public user=... model=... prompt=2026-10-23.2 prompt_sha=...
messages=... tool_calls=... latency=... tokens_in=... cached=... cache_hit=...
tokens_out=... reasoning_tokens=...
```

`cached` 兼容 OpenAI 的 `prompt_tokens_details.cached_tokens` 与 DeepSeek 的
`prompt_cache_hit_tokens`。

## 5. 工具

| 工具 | 可用会话 | 边界 |
|---|---|---|
| `get_current_time` | 公共 + 私聊 | 只读；默认 `Asia/Hong_Kong` |
| `search_public_messages` | 公共房间 | 只搜当前房间公开消息，最多 10 条 |
| `search_private_messages` | 私聊 | 只搜本人与 Bot 的线程，最多 10 条 |
| `web_search` | 公共 + 私聊，需启用 | 独立 DeepSeek Search；query 1–128 字符，最多 3 个候选，仅发现不算已读 |
| `web_fetch` | 公共 + 私聊，需启用 | 独立 DeepSeek Fetch；本 run 来源 ID 或公开 URL 二选一，有界正文快照 |
| `send_message` | 公共 + 私聊 | 在 final 前发一条短过渡消息；公共最多 1 条、私聊最多 2 条，≤600 字符；写入当前会话并受 run 授权/取消与白名单发布约束，不绑定 `run_id`，不改变最终消息的唯一性 |

约束：默认最多 3 轮工具调用（`CHAT_LLM_MAX_TOOL_ROUNDS`），每轮模型可请求至多 4 项并行调用，服务端按序执行且各项共享轮次预算；超过 4 项拒绝该轮。单个结果截断到 8000 字符，参数上限 8192 字节、单轮 assistant 输出 64KB，工具循环累计新增输入超过 128KB（`MaxRoundGrowthBytes`）即中止该 run；网页工具还有第 7.1 节的更小上限。工具由 run wrapper 在执行前后重验会话与取消，被取消或未授权时模型收到 `run_inactive` 等稳定错误码而非裸 context 错误；所有工具错误统一为「snake_case 代码 + 一句可行动的话」，内部 store/provider 细节只留服务端日志、不回传模型。没有 shell、文件、任意 SQL 或跨用户私聊；`send_message` 只能发到当前 run 的会话，且不携带工具参数或内部数据。

## 6. 成本与失败处理

- LLM HTTP 请求默认超时 90s；当前 Hub 为整个 Bot 回复传入 60s context，因此回复/工具循环还受更短的外层期限约束；默认每次模型输出上限 4096 tokens。
- 工具最多 3 轮，防止模型在失败工具上死循环。
- 免费展示面有硬预算：默认每 actor 50 次/日、每 IP 200 次/日、实例 800 次/日、300 万 tokens/日、并发 4。入队事务在 `bot_runs` 预占完整有界工具循环的 token 和请求数；禁用 SDK 隐式重试。只有全部 usage 可信的成功执行才按实际值结算；未知/部分 usage 保留预占，排队取消未调用模型则 token 结算为零。
- OIDC actor 按 Account sub 归账；访客仍可换 handle，IP/实例总额作为补充。IPv6 按 /64 归并，防止仅换主机位获取新配额。
- 预算日固定为入队时的 HKT 日期并持久化，重启与跨午夜不清掉旧 run 的已用/预占额度。网页工具金额另记持久账本 `bot_web_calls`，不拿 token 上限代替外部调用费；金额和已接纳调用次数规则见第 7.1 节。
- 拒绝时只给发起人一行提示，不向房间广播。
- 模型报错只给用户一句固定提示，不泄露上游细节；结构化日志保留排查字段。
- 公共流式回复只传文字；最终回复与历史可包含文字和安全 `source` parts。reasoning、原始 query、工具参数/结果和网页全文不进入公共消息。旧公共历史读取同样过滤，原文保留；私聊数据只对本人账号可见，前端也隐藏原始 Web 工具卡片，原有非 Web 私聊 parts 保留。

## 7. 网页研究与后续实施方向

先完成单 Bot 的自主交互，再按稳定工具、资料来源或审核职责增加专家 Bot。AgentHub 已退出当前参考范围；跨项目用户、内部运营成员、IAM 和邮箱设计统一见 [Account 平台设计](../../docs/identity/account-platform-design.md)，不在本文件复制另一套身份方案。

| 顺序 | 实施内容 | 关键验收 |
|---|---|---|
| 已有基础 | run actor、房间仲裁、冻结输入、持久预占、取消、单条幂等 final；补齐统一 principal/bot ID 仍属后续 | 当前实现见第 2、6 节；测试/部署状态看验证记录 |
| 已实现网页研究 | DeepSeek Search → provider Fetch → 持久来源与引用；保留当前模型和 SDK | 当前方案见第 7.1 节；本地集成已验证，Chat 生产启用另验收 |
| 2 | 明确绑定 run 的追加指令；按 run + delivery key 多消息发布 | 新指令不丢、不越权改变任务；重试/重连无重复；必要时分多条交付 |
| 3 | 普通人类消息的受控参与 selector；按房间灰度、可静音、计费并限制频率 | 闲聊保持沉默；显式点名优先；Bot 输出不唤醒自己；无新信息不刷屏 |
| 4 | 可复用只读 skill、异步专家 handoff、阶段 owner、选定上下文与总预算；最后加入周期/事件任务 | 技能不授予权限；交接不扩大授权或公开私聊；回环有上限；取消当前任务与禁用未来计划分开 |

Grok 的异步 Bot 消息可与可见交接同时存在。我们的后续实现应让服务端队列负责唤醒，只有对共同任务有意义的交接摘要进入房间；不广播内部工具或推理，也不让 Bot 普通输出互相无限触发。每阶段只有一个负责人，子任务共享总预算并受撤销信号约束。

公开发布与内部执行采用不同数据形状。未来多消息能力不得把模型工具调用前的内部草稿自动转成群聊气泡；公开内容、问题和已授权产物须经过明确发布入口。当前 Web 使用独立 provider API；Python 尚未实现，仍需确认供应商协议或另做适配，不能因工具同名就认为 Chat Completions 网关支持托管工具。

### 7.1 当前只读网页研究与来源合同

保留 `deepseek-v4.1-flash` 与 `github.com/openai/openai-go` v1.12.0，模型继续调用 Chat Completions。`backend/websearch` 使用该 SDK 的 `Client.Post` 独立调用 DeepSeek `/v1/tools/search`、`/v1/tools/fetch`；搜索发 `need_content:false`，正文必须通过 Fetch 获得。[DeepSeek 2026-10-17 公告](https://api-docs.deepseek.com/zh-cn/news/news261017/)正式开放两个 API，[Search/Fetch 指南](https://api-docs.deepseek.com/guides/search/)定义其独立 endpoint。网关已验证两个路由，Chat 生产发布仍需按发布记录确认。

[OpenAI 官方 SDK 文档](https://developers.openai.com/api/docs/libraries)列有 Go 客户端；[OpenAI Web search 文档](https://developers.openai.com/api/docs/guides/tools-web-search)则单独限定支持的模型与 Responses/部分 Chat Completions 用法。使用 OpenAI SDK 只是客户端选择，不代表当前 DeepSeek 模型或任意兼容网关具有 OpenAI 内置搜索能力；本次不升级 SDK，也不更换模型协议。

启用要求 `CHAT_WEB_SEARCH_ENABLED=true`、独立 `CHAT_WEB_SEARCH_API_KEY` 与可用 LLM/run 存储。`CHAT_WEB_SEARCH_BASE_URL` 省略时默认 `https://api.deepseek.com/v1`，指定值须为无凭据、query 或 fragment 的 HTTPS `/v1` base；部署时显式配置并验证两个 endpoint。工具凭据不自动继承模型 key。`capabilities.web_search` 反映实际配置和 adapter 可用性，关闭时不注册工具。

`web_search(query, limit)` 接受 1–128 字符、最多 3 个候选；`web_fetch(source_id | url)` 只接受本 run 注册的来源 ID 或一个通过 `SafeURL` 的公开 URL。模型不能选择 run、授权主体、任意 header/cookie 或发布会话。外部调用只附所需 query 或 URL，不自动附加聊天历史、Account cookie 或用户 IP；prompt 要求只提交最小公开搜索词，把网页指令当作不可信资料。搜索摘要只表示发现，不算读取证据。

`bot_web_sources` 保存服务端来源 ID、run、请求 URL、标题、获取时间、`discovered / fetched / failed` 状态、有界 Markdown 正文快照、SHA-256、截断标记、受控错误码及 extractor。Fetch 由 DeepSeek provider 执行；快照是实际传给模型的截断内容，不是完整网页归档。API 不提供最终重定向 URL，代码也不保存或虚构 `final_url`。同 run 已读取来源可复用，不建立跨 run/用户共享正文缓存。

最终提交从本 run 的持久来源重建 `source` parts，忽略模型自造来源元数据。只有成功 `fetched` 的 `[[source:SOURCE_ID]]` 才转换为安全 CommonMark 编号链接（`[n](<url>)`），并将 `cited` 标为真；未知、未读、失败或畸形标记被移除，流式阶段隐藏内部标记。普通 Markdown 链接可正常展示，但不产生来源验证标记。“已读取”表示取得了 provider 正文，不保证其中事实正确。

公开 DTO 仅含 `source_id/title/url/status/cited`。前端中英双语展示已读取、仅发现或读取失败，只有已读且被引用的来源显示引用标签；不为不透明 ID 编造引用编号。公共实时、最终与历史不公开原始 query、工具参数/结果或正文；私聊来源沿用账号隔离，前端也隐藏 Web 原始工具卡。取消或会话撤销后，迟到来源不得恢复草稿，刷新从最终历史恢复来源列表。

资源限制为每 run 最多 3 次 provider 调用、每次 15 秒、provider 响应体及 gzip 解压各 1 MiB、正文最多 5000 字符、工具 JSON 最多 7500 字符；还受外层 run 时限与工具轮数约束。SDK 自动重试关闭，API transport 不继承环境代理、不跟随 API endpoint 重定向。`SafeURL` 限定公开 HTTP(S)、常规端口与主机写法，拒绝凭据、内网地址、危险数字 IP 等；Chat 不直接连接页面 URL。provider 内部 DNS、页面重定向、MIME 和正文提取由 provider 负责，当前不声称实现本地 DNS pinning 或本地 reader。

`bot_web_calls` 在发起前持久预占 ¥0.005/次，记录 run、tool call、provider、attempt、费率版本、日期、金额与状态；同 run 调用 ID 唯一。每日默认应用预算为 ¥5（`CHAT_WEB_SEARCH_DAILY_MICRO_CNY=5000000`），配置范围 5000–1000000000，日期沿用 run 入队时 HKT 日。明确的 4xx 拒绝（408 除外）或 HTTP 200 空结果可归零；5xx、超时、取消和无效/未知响应保留预占。失败调用仍占已接纳次数，重启不清账，也不自动重试。

费率版本 `deepseek-search-fetch-cny-2026-10-21` 依据中文公告的各 ¥5/千次；英文指南以美元列价。该本地 CNY 预算用于按固定费率约束应用消耗，不自动换汇，不保证等于供应商或网关最终收费；实际账单以网关为准。公告针对特定客户端 server-side 工具的暂缓计费不被当作本实现免费额度。调用次数硬上限独立生效，模型 token 另计。

**未来更换网页读取 adapter 的条件**：若改为本地网络 reader，须另行实现逐次 DNS/重定向校验、验证后地址 pinning、无环境代理绕行以及下载/解压/MIME/正文提取上限；不能把当前 URL 字符串校验当成本地 SSRF transport。更换其他远程 reader 须明确其数据发送、响应与收费合同。上述条件是替换 adapter 的验收要求，当前没有第二套本地抓取实现。Python 与聊天室 FTS 的结构化来源锚点也仍属后续。

### 7.2 追加指令、参与与公开交付（主动参与已实现，其余后续）

追加指令要显式绑定目标 run 和真实输入 message ID，先持久化，再于工具/模型轮次安全边界读取并 ack。只允许任务授权者更新目标，不让房间任意后来消息劫持当前工作。`steer` 改当前任务、普通 follow-up 创建后续任务、cancel 停当前执行，各自有明确语义。初期不做流式半段工具参数的抢占，也不把模型生成的“用户已同意”当控制事件。

普通消息参与（2026-10-23 已实现，`CHAT_BOT_AMBIENT` 默认关闭）：无工具判断器只读当前房间最近 12 条窗口，返回严格 JSON 的 `reply / ignore` 与理由摘要（日志不含正文）。确定性预过滤先于模型：Bot 自己与 system 消息、线程消息、纯图片消息、@具体他人的消息不进入判断；`@all/@everyone/@here/@channel` 房间别名除外。多人同时发消息由每房间防抖窗口聚合：首条合格消息开窗，房间安静 8 秒后判断，新消息重置安静计时但不越过首条消息后 30 秒的硬上限，一整批突发消息折叠成最多一次判断、一次回复、一次配额扣减；窗口在任何预算检查之前关闭，被冷却/配额/退避/进行中 run 拒绝的批次静默沉没，下一条合格消息重新开窗。控制面为每房间独立冷却（默认 90s，`CHAT_BOT_AMBIENT_COOLDOWN_SEC`）与每小时配额（默认 6，`CHAT_BOT_AMBIENT_PER_HOUR`）、全局单次判断 in-flight、判断失败指数退避（封顶 30 分钟且退还房间冷却）；判断通过后在入队前重查房间无进行中的公开 run，显式 `@` 永远优先。上线路径按本节原判据：先内部房间灰度，用误插话率、漏答显式请求率、每次有用回答成本和重复率决定开放，不根据“像真人”主观感受开启全量。`ask` 动作与按房间灰度开关尚未实现。

多消息发布使用单独的 `delivery_key` 和服务端许可类型：公开回答、问题或批准产物；每次发布重验取消与受众。run 活动不是聊天内容，内部思考与工具调用前草稿不自动变公开气泡。初期保留一次任务的单一 owner；同一事实没有新进展就不重复通知。

### 7.3 技能、专家交接与审批（尚未实现）

Skill 保存适用条件、输入、步骤、验证、输出与审批边界，并有版本；固定方法不等于授予工具权限或读取别人的记忆。先做好“一次可验证研究”，再保存 skill；routine 需要另外保存 owner、时区、触发范围、预算、数据来源、缺数据行为和撤销入口。

专家 handoff 的 envelope 绑定 parent run、目标 bot ID、选择后的上下文/来源 ID、阶段 owner、总预算和取消关系。接收方根据自己的授权重新检查；发送许可不等于读取收方历史，更不能把私聊来源移入公共群。每条 handoff 去重、有深度/次数上限，普通 Bot 输出不自动触发回环。

未来外部写动作先生成可审查的具体提案；审批记录绑定动作参数 hash、主体、作用域、目标和期限。执行前重验授权与参数版本，过期审批不执行旧动作。审批/secret/login 回路留在授权者私密会话，房间仅见必要的“等待负责人确认”。读工具无需为了模拟竞品而额外要求确认；服务端批准范围、sandbox 与网络策略始终独立于模型的 Auto Review。

### 7.4 开源运行时取舍

2026-10-21 实际读取固定 commit 后的取舍如下；仅借机制，不引入并行存储、完整外部 agent 网关或个人工具默认权限。

| 一手来源 | 值得借鉴 | 不采用的部分 |
|---|---|---|
| [OpenClaw runtime](https://github.com/openclaw/openclaw/blob/d2590fe90720b0ed6cae0b2f1fbd62602d082e80/docs/agent-runtime-architecture.md)、[command queue](https://github.com/openclaw/openclaw/blob/d2590fe90720b0ed6cae0b2f1fbd62602d082e80/src/process/command-queue.ts)（MIT） | 自有 agent-core、每 session 串行与全局准入、同 generation 才回收占用 | [一 Gateway 一信任域](https://github.com/openclaw/openclaw/blob/d2590fe90720b0ed6cae0b2f1fbd62602d082e80/docs/gateway/security/trust-model.md) 明确不支持互不信任用户；默认跨会话权限不适合公共 Chat，sessionKey 不是授权 |
| [nanobot loop](https://github.com/HKUDS/nanobot/blob/fbb61a373f47769734a0d47d8f61935ffca84b84/nanobot/agent/loop.py)、[web tools](https://github.com/HKUDS/nanobot/blob/fbb61a373f47769734a0d47d8f61935ffca84b84/nanobot/agent/tools/web.py)（MIT） | follow-up journal、会话单 worker、source 元数据、逐跳 URL 检查 | 整套 Python gateway、workspace 全局 memory、抓取多次与可选第三方 reader；输出截断不能代替下载上限 |
| [pi-mono agent loop](https://github.com/badlogic/pi-mono/blob/4c25119f5432dbaa4942ad329642aa0cfecae385/packages/agent/src/agent-loop.ts)（MIT） | 小模型/工具循环、steer 与 follow-up 分离、参数校验后 preflight hook、abort 检查 | hook 是扩展点，不是现成授权系统；个人编码 agent 的使用边界不能替代 Account 与公共受众隔离 |

本轮未移植第三方应用代码。以后若复制实质代码须保留 MIT notice，逐项核对依赖许可；项目 AGPL 与源码发布合同不变。博客解释设计动机，当前实现只以固定源码为证：[pi 作者文章](https://mariozechner.at/posts/2025-11-30-pi-coding-agent/)、[Anthropic 工具设计](https://www.anthropic.com/research/writing-tools-for-agents)。

## 8. 观测与验收

- `GET /api/config`：`bot_name`、`model`、`llm_enabled`、`capabilities.run_control`、`capabilities.web_search`、身份入口开关与消息长度。能力字段依据实际配置、adapter 和持久存储，不仅按工具名字宣告可用。
- 日志：模式、用户、模型、prompt 版本与指纹、消息条数、工具调用数、延迟、token、
  缓存命中。
- 回归：`hub/history_test.go`（隔离、窗口、锚点）、`hub/tools_test.go`（检索隐私）、
  `hub/hub_test.go`（prompt 静态性、mention、typing 作用域、并发槽、限流）、
  `llm/llm_test.go`（SDK 流式与工具循环）。
- 公开输出与引用/线程回归：`hub/public_test.go`、`llm/public_test.go`、`store/public_test.go`、
  `hub/threads_test.go` 及 handler/store 集成测试，覆盖 live/done/旧历史过滤、跨会话父子消息、伪造回复元数据和未知收件人。
- run 回归：`hub/runs_test.go`、`store/runs_test.go` 覆盖同房间队列、冻结上界、跨标签页 owner 取消、迟到工具结果、Account 撤销、重启/跨午夜预算和唯一 final；前端 `botRunState` 与 BotRunStrip 测试覆盖终态不倒退、气泡关联和控制入口。
- Web 回归覆盖 `websearch` 的 URL/响应限制、`hub/web_test.go` 的调用与取消、`store/web_test.go` 的来源引用和金额结算；provider 抓取与未来本地 reader 的验收边界按第 7.1 节区分。
- 2026-10-21 前端 583 tests、typecheck、build 通过，lint 0 error / 47 既有 warning；20 组浏览器验证覆盖桌面/窄屏、明暗、中英、来源注入、刷新去重、取消及公私聊切换。真实 Go Hub + SQLite + 本地 HTTPS provider 桩验证 Search → Fetch → 持久来源 → 编号引用和刷新恢复，未替代生产 provider 验收。Service Worker 为 v12；最终生产状态看发布记录。
