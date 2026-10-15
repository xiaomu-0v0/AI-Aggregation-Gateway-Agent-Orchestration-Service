# Chat Completions API 对接文档

**版本：** v0.1

**状态：** 接口设计阶段

## 1. 接口说明

用于接收用户的聊天消息，并返回 AI 助手的回复。

**请求方式：** `POST`

**请求路径：** `/v1/chat/completions`

**Content-Type：** `application/json`

当前版本先完成非流式对话。后续版本增加 SSE 流式响应。

## 2. 请求参数

| 字段 | 类型 | 必填 | 说明 |
| -------------------- | ------- | -- | ------------------------------------- |
| `model` | string | 是 | 模型标识，例如 `chat-default` |
| `messages` | array | 是 | 按对话顺序排列的消息列表，不能为空 |
| `messages[].role` | string | 是 | 消息角色，初版支持 `system`、`user`、`assistant` |
| `messages[].content` | string | 是 | 消息正文 |
| `stream` | boolean | 否 | 是否使用流式响应，默认 `false` |

第一版只支持文本消息。`stream=true` 暂时返回明确的“不支持该功能”错误，后续实现 SSE 后再开放。

**请求示例：**

`{"model":"chat-default","messages":[{"role":"user","content":"你好，请介绍一下 Go"}],"stream":false}`

## 3. 成功响应

HTTP 状态码：`200 OK`

| 字段 | 类型 | 说明 |
| --------------------------- | ------- | --------------------- |
| `id` | string | 本次生成请求的唯一标识 |
| `object` | string | 固定为 `chat.completion` |
| `created` | integer | 创建时间，Unix 时间戳，单位为秒 |
| `model` | string | 实际使用的模型标识 |
| `choices` | array | 模型生成的候选结果 |
| `choices[].index` | integer | 候选结果索引，初版固定为 `0` |
| `choices[].message.role` | string | 固定为 `assistant` |
| `choices[].message.content` | string | 模型生成的文本 |
| `choices[].finish_reason` | string | 初版正常完成时为 `stop` |
| `usage.prompt_tokens` | integer | 输入 token 数 |
| `usage.completion_tokens` | integer | 输出 token 数 |
| `usage.total_tokens` | integer | 输入与输出 token 总数 |

在模拟响应阶段，token 统计可以暂时使用占位值；接入模型后，应优先使用模型厂商返回的真实统计信息，不应把占位值当成真实用量。

## 4. 错误响应

错误响应使用统一结构，便于调用方处理。

`{"error":{"message":"messages cannot be empty","type":"invalid_request_error","param":"messages","code":"invalid_request"}}`

| HTTP 状态码 |   | 场景             |   |
| -------- | - | -------------- | - |
| `400`    |   | 参数错误或暂不支持的请求参数 |   |
| `502`    |   | 下游模型返回异常响应     |   |
| `503`    |   | 模型服务暂时不可用或熔断拒绝 |   |
| `504`    |   | 模型调用超时         |   |

具体错误状态和错误码将在实现阶段统一。

## 5. 第一版不支持的功能

工具调用、多模态输入、多个候选结果、自定义采样参数、SSE 流式响应暂不支持，后续按项目计划逐步增加。

## 6. 验收标准

1. 能通过 HTTP 调用 `/v1/chat/completions`。
2. 能解析 `model` 与 `messages`。
3. 能校验空消息列表和不支持的 `stream` 请求。
4. 能返回约定的成功 JSON 结构。
5. 能返回约定的错误结构。
6. HTTP 层不直接依赖具体模型厂商的 SDK。
