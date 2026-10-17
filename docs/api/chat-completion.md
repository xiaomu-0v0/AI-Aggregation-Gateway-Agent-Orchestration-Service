# Chat Completions API 对接文档

**版本：** v0.3

**状态：** 已实现（非流式 + SSE 流式），已接入通义千问真实模型

## 1. 接口说明

用于接收用户的聊天消息，并返回 AI 助手的回复。

**请求方式：** `POST`

**请求路径：** `/v1/chat/completions`

**Content-Type：** `application/json`（流式请求同样是 JSON 请求体）

调用链：客户端 → `gateway`（HTTP，OpenAI 兼容契约）→ gRPC → `chat_service` → `data.QwenProvider` → 阿里云百炼（DashScope）。

支持两种响应模式，由请求体中的 `stream` 决定：

| `stream` | 响应类型 | 说明 |
| --- | --- | --- |
| `false`（默认） | `application/json` | 一次性返回完整结果，网关侧时限 15s |
| `true` | `text/event-stream` | 按块推送增量内容，以 `data: [DONE]` 结束，网关侧时限 60s |

> **参数校验先于流式响应。** 请求体非法、`model` 缺失、`messages` 为空、角色不支持等错误，
> 即使 `stream=true` 也会以普通 JSON 返回 `4xx`，不会先返回 `200` 再报错。
> 因此客户端应检查响应状态码与 `Content-Type` 再决定按 JSON 还是按 SSE 解析。

## 2. 请求参数

| 字段 | 类型 | 必填 | 说明 |
| -------------------- | ------- | -- | ------------------------------------- |
| `model` | string | 是 | 逻辑模型标识，当前仅支持 `chat-default` |
| `messages` | array | 是 | 按对话顺序排列的消息列表，不能为空 |
| `messages[].role` | string | 是 | 消息角色，支持 `system`、`user`、`assistant` |
| `messages[].content` | string | 是 | 消息正文，不可为空或纯空白 |
| `stream` | boolean | 否 | 是否使用流式响应，默认 `false` |

当前只支持文本消息。

### 2.1 模型标识说明

`model` 是**逻辑模型名**，不是厂商真实模型名。当前只接受 `chat-default`，由服务端环境变量
`QWEN_MODEL`（默认 `qwen-plus`）决定背后实际调用的模型。

这样设计是为了后续实现多模型路由时，对外契约保持不变。传入其他值会被拒绝。

**非流式请求示例：**

`{"model":"chat-default","messages":[{"role":"user","content":"你好，请介绍一下 Go"}],"stream":false}`

**流式请求示例：**

`{"model":"chat-default","messages":[{"role":"user","content":"你好，请介绍一下 Go"}],"stream":true}`

## 3. 非流式成功响应

HTTP 状态码：`200 OK`，`Content-Type: application/json`

| 字段 | 类型 | 说明 |
| --------------------------- | ------- | --------------------- |
| `id` | string | 模型返回的本次生成请求唯一标识 |
| `object` | string | 固定为 `chat.completion` |
| `created` | integer | 创建时间，Unix 时间戳，单位为秒 |
| `model` | string | 回显请求中的逻辑模型名，**不是上游真实模型名** |
| `choices` | array | 模型生成的候选结果 |
| `choices[].index` | integer | 候选结果索引，当前固定为 `0` |
| `choices[].message.role` | string | 固定为 `assistant` |
| `choices[].message.content` | string | 模型生成的文本 |
| `choices[].finish_reason` | string | 由模型返回，正常完成为 `stop` |
| `usage.prompt_tokens` | integer | 输入 token 数，取自模型返回的真实统计 |
| `usage.completion_tokens` | integer | 输出 token 数，取自模型返回的真实统计 |
| `usage.total_tokens` | integer | 输入与输出 token 总数，取自模型返回的真实统计 |

当前仅返回单个候选结果，`choices` 数组长度恒为 `1`。

`usage` 为模型厂商返回的真实用量，可用于计费与配额核算。

## 4. 流式成功响应（SSE）

HTTP 状态码：`200 OK`

响应头：

| 响应头 | 值 |
| --- | --- |
| `Content-Type` | `text/event-stream; charset=utf-8` |
| `Cache-Control` | `no-cache` |
| `X-Accel-Buffering` | `no` |

响应体为标准 Server-Sent Events，每条事件形如 `data: <JSON>\n\n`。

### 4.1 帧结构

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | 本次流的唯一标识，同一流内所有帧一致 |
| `object` | string | 固定为 `chat.completion.chunk` |
| `created` | integer | 创建时间，Unix 时间戳，单位为秒，同一流内一致 |
| `model` | string | 回显请求中的逻辑模型名 |
| `choices` | array | 长度恒为 `1` |
| `choices[].index` | integer | 固定为 `0` |
| `choices[].delta.role` | string | 仅首帧出现，值为 `assistant` |
| `choices[].delta.content` | string | 增量文本，可能为空（此时字段省略） |
| `choices[].finish_reason` | string \| null | 未结束时为 `null`，终止帧为 `stop` 等 |

> `id` 由网关生成（形如 `chatcmpl-<UnixNano>`），与上游模型返回的 ID 无关。
> 这一点与非流式响应不同——后者直接透传模型返回的 ID。

### 4.2 完整示例

```
data: {"id":"chatcmpl-1730000000000000000","object":"chat.completion.chunk","created":1730000000,"model":"chat-default","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-1730000000000000000","object":"chat.completion.chunk","created":1730000000,"model":"chat-default","choices":[{"index":0,"delta":{"content":"Go"},"finish_reason":null}]}

data: {"id":"chatcmpl-1730000000000000000","object":"chat.completion.chunk","created":1730000000,"model":"chat-default","choices":[{"index":0,"delta":{"content":" 是"},"finish_reason":null}]}

data: {"id":"chatcmpl-1730000000000000000","object":"chat.completion.chunk","created":1730000000,"model":"chat-default","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

### 4.3 流式协议约定

1. 建立连接后**先发送一帧只含 `delta.role` 的事件**，客户端据此可确认流已开始。
2. 之后逐帧推送 `delta.content`，内容需要在客户端**按序拼接**。
3. 终止帧的 `finish_reason` 非 `null`，此时 `delta` 可能为空对象。
4. 以 `data: [DONE]` 表示正常结束，客户端收到后应停止读取。
5. 空行、注释行以及非 `data:` 开头的行都应被忽略。

### 4.4 流式响应中的 token 用量

**当前版本流式响应不返回 `usage`。** 上游已开启 `stream_options.include_usage`，
但 token 统计在数据层解析后未继续向上传递，因此流式调用暂时拿不到用量。

需要精确计费时，请先使用非流式接口，或等待后续版本补齐。

## 5. 错误响应

### 5.1 错误结构

非流式错误与流式开始前的错误，均使用统一 JSON 结构：

```json
{
  "error": {
    "message": "messages cannot be empty",
    "type": "invalid_request_error",
    "param": "messages",
    "code": "invalid_request"
  }
}
```

| 字段 | 说明 |
| -------- | -------------------------------- |
| `message` | 面向开发者的英文错误描述 |
| `type` | 错误大类，`invalid_request_error` 或 `server_error` |
| `param` | 出错的请求参数名，无明确参数时为空串 |
| `code` | 机器可读的细分错误码，见下表 |

### 5.2 错误码对照表

| HTTP | `type` | `code` | `param` | 触发场景 |
| ---- | ------ | ------ | ------- | -------- |
| `400` | `invalid_request_error` | `invalid_request` | （空） | 请求体不是合法 JSON |
| `400` | `invalid_request_error` | `invalid_request` | `model` | `model` 缺失或为空白 |
| `400` | `invalid_request_error` | `invalid_request` | `messages` | `messages` 为空数组 |
| `400` | `invalid_request_error` | `invalid_request` | `messages` | 某条消息 `content` 为空或纯空白 |
| `400` | `invalid_request_error` | `invalid_role` | `messages` | 消息 `role` 不在支持范围内 |
| `500` | `server_error` | `stream_unsupported` | （空） | 服务端 ResponseWriter 不支持 Flush |
| `502` | `server_error` | `upstream_error` | （空） | 下游模型返回异常响应，或流式请求无法建立 |
| `503` | `server_error` | `service_unavailable` | （空） | 下游不可用，或熔断器已打开 |
| `504` | `server_error` | `timeout` | （空） | 下游调用超时（非流式，网关侧上限 15s） |

> 注意：传入不支持的 `model` 目前会被归入 `502 upstream_error`，语义上更接近客户端参数错误。
> 待统一错误分类后修正为 `400`。

### 5.3 流式响应中途的错误

一旦响应头以 `200` 发出，HTTP 状态码就无法再更改。此时链路错误通过一条 SSE 事件表达：

```
data: {"error":{"message":"chat stream interrupted","type":"server_error","param":"","code":"stream_interrupted"}}
```

| `code` | 触发场景 |
| --- | --- |
| `stream_interrupted` | 流已开始后，下游模型异常、超时（60s）或链路中断 |

客户端处理建议：收到该事件后应视为本次生成失败，**不要**把已拼接的内容当作完整答案。

### 5.4 调用方建议

- `400`：请求本身有问题，重试无意义，应修正参数。
- `500` / `502` / `503` / `504`：属于下游或链路问题，可配合退避策略重试。
- 熔断已打开时（`503` + `service_unavailable`），网关内部的 gRPC 重试会主动跳过，避免放大故障。
- 流式请求建议设置客户端读超时略大于 60s，避免网关侧超时后客户端仍在等待。

## 6. 暂不支持的功能

工具调用、多模态输入、多个候选结果、自定义采样参数（`temperature`、`top_p` 等）、流式响应的 token 用量透出，暂不支持，后续按项目计划逐步增加。

## 7. 验收标准

| # | 标准 | 状态 |
| - | ---- | ---- |
| 1 | 能通过 HTTP 调用 `/v1/chat/completions` | 已达成 |
| 2 | 能解析 `model` 与 `messages` | 已达成 |
| 3 | 能校验空消息列表和非法请求参数 | 已达成 |
| 4 | 能返回约定的成功 JSON 结构 | 已达成 |
| 5 | 能返回约定的错误结构 | 已达成 |
| 6 | HTTP 层不直接依赖具体模型厂商的 SDK | 已达成（厂商细节封装在 `internal/data`） |
| 7 | 能对接真实模型并返回真实 token 用量（非流式） | 已达成（通义千问） |
| 8 | 能以 SSE 流式返回增量内容，并以 `[DONE]` 结束 | 已达成 |
| 9 | 流式响应能透出 token 用量 | 未达成，见 4.4 |

## 8. 已知限制

- 流式响应不返回 `usage`，无法用于精确计费（见 4.4）。
- 逻辑模型名只有 `chat-default` 一个，传入其他值会被拒绝。
- 不支持的模型名返回 `502 upstream_error` 而非 `400`，错误分类待统一。
- 响应 `model` 字段回显请求值，未透出上游真实模型名。
- 超时值分散且不一致：非流式 15s、流式 60s、Provider HTTP 客户端 60s。
