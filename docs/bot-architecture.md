# TokenBot Agent 架构

最后更新：2026-10-20　适用代码：`backend/hub`、`backend/llm`　Prompt：`2026-10-20.4`

这份文档描述**当前真实行为**、取舍依据和升级条件。先给结论，再给调研证据。

## 结论

| 问题 | 决定 |
|---|---|
| 群聊还是单聊 | 同一个 Bot，两种会话：公共房间显式 `@`，私聊直接对话 |
| 群里什么时候说话 | 只被点名（`@` 或回复 Bot 的消息）才说；不猜、不主动插话 |
| 记忆 | SQLite transcript 是唯一真相源；近期窗口 + FTS 检索，同一份数据两种读法 |
| 要不要记忆框架 | 不要。现在的问题是"找回原话"，不是"跨会话画像" |
| Prompt | 每个模式逐字节静态，带版本号和 SHA；易变信息只放在 user turn 里 |
| 缓存 | 对话窗口用锚点追加，不逐条滑动；只有溢出时才付一次 cache miss |
| 工具 | 只暴露 3 个只读工具；没有 shell、文件、网络、任意 SQL |
| 多 Bot | 不做。等出现稳定的专家角色和审批边界再拆 |

## 1. 调研结论

### Grok Bot：群聊里"点名 + 单一 owner"

Grok Bot 的群聊不是"谁说话都接"的陪聊机器人：

- 在群里写 `@Bot` 才把请求定向给某个 Bot；普通消息不会触发。
- 每个阶段只保留一个 owner，handoff 本身需要可见时才放进群聊。
- 私聊里 Bot 会回复每一条消息，因为 1:1 没有"是不是在问我"的歧义。
- Slack 频道线程里，个人 Bot 不能代表某个人的身份执行，也不会私聊别的 Bot；
  Team Bot 才会代表点名它的成员，并把回复留在同一个线程。

这直接决定了我们的模型：**公共房间是 Team Bot，私聊是个人 Bot**。房间里说的话所有人
都能看到，私聊的东西永远不会回流到房间。

参考：

- <https://docs.x.ai/grok-bot/chat-and-collaboration>
- <https://prod.cursor.com/help/grok-bot/group-chats>
- <https://x.ai/news/designing-grok-bot>

### Slack：不能使用调用者本来看不到的数据

Slack 的 agent 设计规范把"数据边界"写成硬要求：调用者自己打不开的文件、频道、记录，
agent 不能拿来生成回答；同时必须让用户清楚知道自己在和 AI 交互。

落到我们这里：

- 房间检索强制 `room_id = 当前房间` 且 `to_user = ''`。
- 私聊检索强制只匹配「本人 ↔ TokenBot」，SQL 同时限制说话人和收件人。
- Prompt 明确写出记忆范围，不允许模型假装记得别的房间或别人的私聊。

参考：<https://docs.slack.dev/concepts/agent-design/>

### MUCA：群聊的三个难点是 What / When / Who

多用户聊天助手的研究把问题拆成：回答什么、什么时候回答、对谁说。显式 `@` 一次解决
后两个，代价是用户要主动点名；换来的是零误触发、零刷屏。对"叫到才干活"的产品定位，
这比训练一个插话判定器更可靠，也更便宜。

参考：<https://arxiv.org/abs/2401.04883>

### Session 框架：历史存储不等于记忆策略

OpenAI Agents SDK 的 session 本质上是"把对话历史持久化并在下一轮带回来"，需要压缩时
再替换成更短的等价历史。这和我们"用消息表做记忆"是同一件事，差别只是存储位置。我们
的消息本来就在 SQLite 里，用户也能翻到，所以没有理由再引入第二套会话存储。

参考：<https://openai.github.io/openai-agents-python/sessions/>

### 前缀缓存：静态在前，动态在后，窗口不要滑

2026 年的缓存实践基本收敛到同一条：provider 按请求前缀匹配缓存，system prompt、工具
定义和工具顺序必须稳定；时间、用户名、检索结果这类动态内容放到后面。针对长任务的
研究还发现，粗暴地把全部上下文塞进缓存，反而可能增加延迟。

因此：

- system prompt 按模式静态生成，不含时钟、说话人、在线人数、模型名。
- 工具按固定顺序注册，签名变化视为 prompt 变化，必须 bump 版本。
- 对话窗口用锚点追加（见第 3 节），而不是每来一条就整体前移。

参考：

- <https://developers.openai.com/api/docs/guides/prompt-caching>
- <https://arxiv.org/abs/2601.06007>
- <https://docs.aws.amazon.com/wellarchitected/latest/agentic-ai-lens/agentcost02-bp03.html>

### 工具：三个就够，工具面就是攻击面

公开的工具体验数据很一致：工具定义会实打实吃 context，几十个工具就能占掉数万 token，
还会降低选择准确率。静态工具集应尽量控制在 20 个以内，超过再考虑按需发现。

我们只有三个只读工具，没有"先加着以后可能用"的预留。

参考：<https://nango.dev/blog/how-to-expose-tools-to-agents-without-clogging-context>

## 2. 交互模型

| 场景 | 触发方式 | 上下文 | 回复可见性 |
|---|---|---|---|
| 公共房间 | `@TokenBot`，或回复 TokenBot 的消息 | 本房间 transcript | 广播给房间 |
| 私聊 | 向 TokenBot 发送消息 | 本人与 TokenBot 的 transcript | 只回给本人 |

公共房间只有被点名才回复。不做"这句话像不像在问我"的判定，也不做主动插话：误判一次
就是刷屏，而用户重新 `@` 一次的成本很低。

### 并发与节流

- 同一个用户的公共触发有 3 秒冷却；私聊与公共房间各自计时。
- 每个会话（`bot:<user>` / `dm:<user>`）同时只允许一个回复在跑，跨标签页也生效。
- 每条回复带唯一 `stream_id`，两段流式输出不会被前端合并成一条。
- 公共房间的 typing 只发给当前房间；私聊 typing 只发给收件人。
- 房间冷却期内静默跳过；私聊会明确提示"还在回复上一条"。

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
- 同一个 Bot 对不同用户是独立历史；共享的只有 system prompt 和工具定义。
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
- `TokenBotPromptVersion` 由 `/api/config` 暴露；`prompt_sha` 给出两个模式的短指纹。
- 每轮日志带 `prompt`、`prompt_sha`、`cache_hit`，用于发现"没改代码但前缀变了"。

每轮日志：

```
bot reply mode=public user=... model=... prompt=2026-10-20.4 prompt_sha=...
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

约束：最多 3 轮工具调用（`CHAT_LLM_MAX_TOOL_ROUNDS`），关闭并行工具调用，单个结果截断
到 8000 字符。没有 shell、文件、网络、任意 SQL、跨用户私聊、主动发消息。

## 6. 成本与失败处理

- 请求超时 90s，单轮 60s，最大输出 4096 tokens。
- 工具最多 3 轮，防止模型在失败工具上死循环。
- 免费展示面有硬预算：默认每用户 50 次/日、每 IP 200 次/日、实例 800 次/日、
  300 万 tokens/日、并发 4。调用前预占，调用后按实际 token 记账。
- 访客可以随时换名字，所以每用户限额只是体验层；**每 IP 与实例总额才是成本边界**。
- 预算在进程内按 HKT 自然日重置；重启清零，仅适用于当前单实例部署。
- 拒绝时只给发起人一行提示，不向房间广播。
- 模型报错只给用户一句固定提示，不泄露上游细节；结构化日志保留排查字段。
- Bot 回复落库时保存 assistant-ui parts（文字、推理、工具卡），刷新不丢。

## 7. 多用户与多 Bot

公共房间天然是多用户共享上下文；私聊按用户隔离。

现在不引入多 Bot。只有当出现**稳定的专家角色**（不同工具、数据源、审批边界）时才值得
拆分。Grok Bot 的做法也是先让一个 Bot 端到端负责，再按专家角色增加；群聊只在 handoff
本身需要可见时使用，每个阶段指定单一 owner。到那一步，handoff 应该是一条可见的事件卡，
而不是让模型在后台互相调用。

## 8. 观测与验收

- `GET /api/config`：`bot_name`、`model`、`llm_enabled`、`prompt_version`、
  `prompt_sha`、`capabilities`、`limits`。
- 日志：模式、用户、模型、prompt 版本与指纹、消息条数、工具调用数、延迟、token、
  缓存命中。
- 回归：`hub/history_test.go`（隔离、窗口、锚点）、`hub/tools_test.go`（检索隐私）、
  `hub/hub_test.go`（prompt 静态性、mention、typing 作用域、并发槽、限流）、
  `llm/llm_test.go`（SDK 流式与工具循环）。
