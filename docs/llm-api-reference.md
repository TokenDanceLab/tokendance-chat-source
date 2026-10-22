# TokenBot LLM Contract

TokenBot 只依赖一个协议：OpenAI Chat Completions（`POST {base}/chat/completions`），
通过官方 Go SDK `github.com/openai/openai-go` 调用。OpenAI、DeepSeek、TokenDance
Gateway 都走同一条路径；`CHAT_LLM_PROVIDER` 只是运维标签，不切换协议。

## 请求形状

| 项 | 取值 |
|---|---|
| 端点 | `POST /chat/completions`（`CHAT_LLM_BASE_URL` 需包含版本段，如 `https://api.deepseek.com/v1`） |
| 认证 | `Authorization: Bearer $CHAT_LLM_API_KEY` |
| system | 一条 `role: "system"` 消息，由 `hub.BuildPrompt` 生成 |
| 输出上限 | `max_tokens`（`CHAT_LLM_MAX_TOKENS`，默认 4096） |
| 采样 | `temperature` / `top_p`，未配置则沿用服务端默认 |
| 思考 | `reasoning_effort`（`low` / `medium` / `high`） |
| 工具 | `tools[].function` + `tool_choice: auto`，单轮串行执行 |
| 流式 | `stream: true`，SSE `data: {...}` 分片，以 `data: [DONE]` 结束 |

## 流式分片

```json
{"choices":[{"delta":{"reasoning_content":"..."}}]}
{"choices":[{"delta":{"content":"..."}}]}
{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_current_time","arguments":"{\"timezone\":"}}]}}]}
```

- `reasoning_content`（DeepSeek / Gateway）与 `reasoning` 都映射到 assistant-ui 的
  `reasoning` part。SDK 不模型化该字段，`llm.reasoningDelta` 从 chunk 的
  `RawJSON()` 读取。
- `tool_calls[].function.arguments` 是逐片拼接的 JSON 字符串，拼完才执行工具。
- 工具结果以 `role: "tool"` 消息回传，再发下一轮请求，最多
  `CHAT_LLM_MAX_TOOL_ROUNDS`（默认 3）轮。

## 消息与 parts

一轮回复落库为 `messages.parts`（JSON），并同时广播给前端：

| part | 字段 | 渲染 |
|---|---|---|
| `text` | `text` | Markdown 正文 |
| `reasoning` | `text` | 可折叠「思考过程」卡片 |
| `tool-call` | `tool_call_id` `tool_name` `arguments` `result` `status` | 可折叠工具卡（running / complete / error） |

`messages.content` 仍是纯文本（仅 text parts 拼接），用于搜索、复制、标题与兼容旧客户端。

## 工具

| 工具 | 可用范围 | 说明 |
|---|---|---|
| `get_current_time` | 公共 + 私聊 | IANA 时区，默认 `Asia/Hong_Kong` |
| `search_public_messages` | 仅公共房间 | FTS5 搜索，SQL 强制 `to_user = ''`，不返回私聊 |

## 上下文与缓存

- **数据库即记忆**：每轮从 `messages` 读取当前会话最近 `CHAT_LLM_MEMORY_SIZE` 条
  （默认 20），公共房间按 `room_id`、私聊按 `to_user` 隔离。没有并行的内存窗口，
  重启不失忆，也不会把一个房间或一个私聊的上下文带进另一个。
- **系统提示词按模式静态生成**：不含时间、说话人、在线人数或模型名。用户消息在
  转录里带 `@name · YYYY-MM-DD HH:mm` 前缀，历史前缀因此保持逐字节稳定，
  DeepSeek / OpenAI 的前缀缓存可以持续命中。
- **不做自动摘要**，也不引入向量库：聊天规模下 FTS5 检索工具已覆盖长程回忆，
  摘要会引入额外成本与人格漂移。
- 每轮回复记录一行结构化日志：`mode user model prompt messages tool_calls
  latency tokens_in cached cache_hit tokens_out reasoning_tokens`，用于观察缓存命中率。
