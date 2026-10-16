# Chat Completions API 对接文档

**版本：** v0.2

**状态：** 已实现（非流式），已接入通义千问真实模型

## 1. 接口说明

用于接收用户的聊天消息，并返回 AI 助手的回复。

**请求方式：** `POST`

**请求路径：** `/v1/chat/completions`

**Content-Type：** `application/json`

当前版本先完成非流式对话。后续版本增加 SSE 流式响应。

调用链：客户端 → `gateway`（HTTP，OpenAI 兼容契约）→ gRPC → `chat_service` → `data.QwenProvider` → 阿里云百炼（DashScope）。

## 2. 请求参数

| 字段 | 类型 | 必填 | 说明 |
| -------------------- | ------- | -- | ------------------------------------- |
| `model` | string | 是 | 逻辑模型标识，当前仅支持 `chat-default` |
| `messages` | array | 是 | 按对话顺序排列的消息列表，不能为空 |
| `messages[].role` | string | 是 | 消息角色，支持 `system`、`user`、`assistant` |
| `messages[].content` | string | 是 | 消息正文，不可为空或纯空白 |
| `stream` | boolean | 否 | 是否使用流式响应，默认 `false` |

第一版只支持文本消息。`stream=true` 返回明确的“不支持该功能”错误，后续实现 SSE 后再开放。

### 2.1 模型标识说明

`model` 是**逻辑模型名**，不是厂商真实模型名。当前只接受 `chat-default`，由服务端环境变量
`QWEN_MODEL`（默认 `qwen-plus`）决定背后实际调用的模型。

这样设计是为了后续实现多模型路由时，对外契约保持不变。传入其他值会被拒绝。

**请求示例：**

`{"model":"chat-default","messages":[{"role":"user","content":"你好，请介绍一下 Go"}],"stream":false}`

## 3. 成功响应

HTTP 状态码：`200 OK`

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

## 4. 错误响应

错误响应使用统一结构，便于调用方处理。

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

### 4.1 错误码对照表

| HTTP | `type` | `code` | `param` | 触发场景 |
| ---- | ------ | ------ | ------- | -------- |
| `400` | `invalid_request_error` | `invalid_request` | （空） | 请求体不是合法 JSON |
| `400` | `invalid_request_error` | `invalid_request` | `model` | `model` 缺失或为空白 |
| `400` | `invalid_request_error` | `invalid_request` | `messages` | `messages` 为空数组 |
| `400` | `invalid_request_error` | `invalid_request` | `messages` | 某条消息 `content` 为空或纯空白 |
| `400` | `invalid_request_error` | `invalid_role` | `messages` | 消息 `role` 不在支持范围内 |
| `400` | `invalid_request_error` | `unsupported_value` | `stream` | `stream=true`（暂不支持） |
| `502` | `server_error` | `upstream_error` | （空） | 下游模型返回异常响应或未知错误 |
| `503` | `server_error` | `service_unavailable` | （空） | 下游不可用，或熔断器已打开 |
| `504` | `server_error` | `timeout` | （空） | 下游调用超时（网关侧上限 15s） |

> 注意：传入不支持的 `model` 目前会被归入 `502 upstream_error`，语义上更接近客户端参数错误。
> 待统一错误分类后修正为 `400`。

### 4.2 调用方建议

- `400`：请求本身有问题，重试无意义，应修正参数。
- `502` / `503` / `504`：属于下游或链路问题，可配合退避策略重试。
- 熔断已打开时（`503` + `service_unavailable`），网关内部的 gRPC 重试会主动跳过，避免放大故障。

## 5. 第一版不支持的功能

工具调用、多模态输入、多个候选结果、自定义采样参数（`temperature`、`top_p` 等）、SSE 流式响应暂不支持，后续按项目计划逐步增加。

## 6. 验收标准

| # | 标准 | 状态 |
| - | ---- | ---- |
| 1 | 能通过 HTTP 调用 `/v1/chat/completions` | 已达成 |
| 2 | 能解析 `model` 与 `messages` | 已达成 |
| 3 | 能校验空消息列表和不支持的 `stream` 请求 | 已达成 |
| 4 | 能返回约定的成功 JSON 结构 | 已达成 |
| 5 | 能返回约定的错误结构 | 已达成 |
| 6 | HTTP 层不直接依赖具体模型厂商的 SDK | 已达成（厂商细节封装在 `internal/data`） |
| 7 | 能对接真实模型并返回真实 token 用量 | 已达成（通义千问） |
