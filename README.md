# AI 聚合网关 / Agent 编排服务

基于 [Go Kratos v2](https://github.com/go-kratos/kratos) 构建的 AI 聚合网关与 Agent 编排服务。

对外提供 **OpenAI 兼容**的 HTTP 对话接口，对内通过 gRPC + Consul 组网，支持同机多实例水平扩展。

> **当前进度**：非流式与 **SSE 流式**对话链路均已打通，并已接入**通义千问（DashScope）**真实模型。
> 两种模式都能返回真实 token 用量（流式需客户端显式开启 `stream_options.include_usage`）。
> 超时链路已按「内层短、外层长」逐层收敛。

---

## 架构

```
                     HTTP POST /v1/chat/completions
                     （stream=false → JSON，stream=true → SSE）
    客户端 ─────────────────────────────────────────────┐
                                                        ▼
                                            ┌───────────────────────┐
                                            │        gateway        │
                                            │   对外 HTTP  :8080     │
                                            │  OpenAI 兼容 JSON 契约 │
                                            │    服务端超时已关闭    │
                                            └───────────┬───────────┘
                                                        │ gRPC
                                    服务发现 discovery:///chat_service
                                    熔断 · 元数据透传 · 逐层超时
                                     一元调用 / 服务端流式调用
                                                        │
                                                        ▼
                                            ┌───────────────────────┐
                                            │     chat_service      │
                                            │   内网 gRPC  :9000     │
                                            │   内网 HTTP  :8000     │
                                            │   Chat / Greeter 服务  │
                                            └───────────┬───────────┘
                                                        │
                              biz.ModelProvider + biz.StreamingModelProvider
                                                        │
                                                        ▼
                                            ┌───────────────────────┐
                                            │    data.QwenProvider  │
                                            │  通义千问 OpenAI 兼容  │
                                            └───────────┬───────────┘
                                                        │ HTTPS
                                                        ▼
                                              DashScope  百炼平台

                  ┌──────────────────────────────────────────────┐
                  │   Consul  127.0.0.1:8500   服务注册与发现      │
                  └──────────────────────────────────────────────┘
```

**设计要点**

| 关注点 | 实现方式 |
| --- | --- |
| 内外协议隔离 | 对外是手写的 OpenAI 兼容 JSON，对内是 protobuf gRPC，二者在 gateway 显式转换 |
| 模型抽象 | `biz.ModelProvider` 接口定义业务层需要的模型能力，`data.QwenProvider` 提供实现，由 Wire 绑定；业务层不依赖任何厂商 SDK，换模型只改 `data` 层 |
| 流式能力 | `biz.StreamingModelProvider` 作为**可选接口**独立存在，`ChatUsecase.Stream` 通过类型断言按需启用；未实现该接口的 Provider 会明确报错而非静默降级，主接口保持干净 |
| 流式透传 | gateway 先用 gRPC 服务端流接收内部 chunk，再逐帧转成 OpenAI 风格 SSE 并 `Flush`，中间不攒批，保证首字延迟 |
| 逐层超时 | 内层短、外层长，避免外层先超时后内层仍在消耗模型配额；网关关掉 Kratos 默认的 1s 服务端超时，改由 Handler 按模式分别控制 |
| 逻辑模型名 | 对外统一暴露 `chat-default`，由 `QWEN_MODEL` 决定背后真实调用的模型，为后续多厂商路由留出扩展位 |
| 服务发现 | Consul；gateway 通过 `discovery:///chat_service` 解析下游实例 |
| 容错 | 客户端熔断（`circuitbreaker`）+ 对 `Unavailable` 的退避重试（最多 3 次，熔断打开时不再重试） |
| 可观测 | 统一日志中间件、`request_id` 透传、metadata 跨进程传递 |
| 多实例 | `chat_service` 以「主机名 + gRPC 端口」作为 Consul 实例 ID，同一台机器可跑多实例 |

### 超时链路

Kratos 的 `transport/grpc` 中 `timeout` **只作用于一元（unary）调用**，流式调用不受该选项约束，
因此两种模式需要分别设计：

| 层级 | 非流式 | 流式 | 说明 |
| --- | --- | --- | --- |
| Provider HTTP 调用 | 60s | 270s | `context.WithTimeout` 控制；`http.Client` 本身不设超时，避免与上下文冲突 |
| gRPC Server（chat_service） | 65s | 不生效 | `configs/config.yaml` 的 `server.grpc.timeout` |
| gRPC Client（gateway） | 70s | 不生效 | `kgrpc.WithTimeout`，仅由一元拦截器使用 |
| Gateway Handler | 70s | 300s | 两种模式各自的 `context.WithTimeout` |
| Gateway HTTP Server | 无 | 无 | `khttp.Timeout(0)` 关闭默认 1s 限制 |

> 关键点：内层必须短于外层，否则外层先返回超时、内层却还在消耗模型配额。

---

## 目录结构

```
.
├── api/                      # protobuf 契约与生成代码
│   ├── chat/v1/              #   内部 Chat 服务（含 StreamChatCompletion 流式 RPC）
│   └── chat_service/v1/      #   Greeter 示例服务
├── cmd/
│   ├── gateway/              # 对外 HTTP 网关（Wire 依赖注入）
│   └── chat_service/         # 内网 gRPC 服务（Wire 依赖注入）
├── configs/                  # 主实例配置
├── configs-instance-2/       # 第二实例配置（多实例演示）
├── docs/api/                 # 接口对接文档
├── internal/
│   ├── biz/                  # 业务用例与模型抽象
│   │   ├── model.go          #   ModelProvider / StreamingModelProvider 接口 + ChatUsecase
│   │   └── greeter.go        #   Greeter 用例
│   ├── conf/                 # 配置结构（protobuf 定义）
│   ├── data/                 # 数据访问与模型实现
│   │   └── qwen.go           #   通义千问 Provider（DashScope HTTP 调用 + SSE 流式解析）
│   ├── gateway/              # 网关专属：client / middleware / server / service
│   ├── middleware/           # 通用日志中间件
│   ├── registry/             # Consul 注册中心接入
│   ├── server/               # HTTP / gRPC server 装配（含端到端集成测试）
│   └── service/              # 业务服务实现
├── third_party/              # protoc 依赖的公共 proto
├── Dockerfile
├── Makefile
└── openapi.yaml              # 由 proto 生成的 OpenAPI 描述
```

---

## 快速开始

### 环境依赖

- Go 1.22+
- Consul（默认 `127.0.0.1:8500`）
- 阿里云百炼（DashScope）API Key
- MySQL / Redis（配置已预留，当前业务尚未实际使用）
- `protoc` 及 Kratos 插件（仅在改动 proto 时需要）

### 1. 安装工具链

```bash
make init
```

### 2. 启动依赖组件

```bash
consul agent -dev
```

### 3. 配置模型访问凭证

`chat_service` 启动时从**环境变量**读取模型配置。必填项缺失会直接启动失败（`panic`），这是刻意设计——避免带着空凭证跑起来、到线上才暴露。

| 环境变量 | 必填 | 说明 |
| --- | --- | --- |
| `DASHSCOPE_API_KEY` | 是 | 阿里云百炼 API Key |
| `QWEN_BASE_URL` | 是 | DashScope OpenAI 兼容模式地址 |
| `QWEN_MODEL` | 否 | 实际调用的模型，默认 `qwen-plus` |
| `INSTANCE_NAME` | 否 | 实例标识，多实例时用于区分响应来源，默认 `chat-unknow` |

PowerShell：

```powershell
$env:DASHSCOPE_API_KEY = "sk-你的key"
$env:QWEN_BASE_URL     = "https://dashscope.aliyuncs.com/compatible-mode/v1"
$env:QWEN_MODEL        = "qwen-plus"
```

Bash：

```bash
export DASHSCOPE_API_KEY="sk-你的key"
export QWEN_BASE_URL="https://dashscope.aliyuncs.com/compatible-mode/v1"
export QWEN_MODEL="qwen-plus"
```

> **不要把密钥写进任何会提交到仓库的文件。** `.gitignore` 已忽略 `.env` 与 `configs/*.local.yaml`，需要固化凭证时请使用这两者之一。代码中**没有任何硬编码密钥**。

### 4. 启动内网服务

```bash
go run ./cmd/chat_service -conf ./configs
```

### 5. 启动对外网关

```bash
go run ./cmd/gateway
```

### 6. 发起非流式调用

```bash
curl -X POST http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"chat-default","messages":[{"role":"user","content":"你好，请用一句话介绍 Go"}],"stream":false}'
```

预期返回：

```json
{
  "id": "chatcmpl-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "object": "chat.completion",
  "created": 1730000000,
  "model": "chat-default",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Go 是 Google 开发的静态强类型、自带并发与垃圾回收的编译型语言。"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": { "prompt_tokens": 18, "completion_tokens": 26, "total_tokens": 44 }
}
```

> `id` 与 `usage` 均来自模型返回的真实数据。
> 响应中的 `model` 字段回显的是**请求里的逻辑模型名**（`chat-default`），不是上游真实模型名；后者目前未对外暴露。

### 7. 发起流式调用

把 `stream` 改为 `true`，并用 `-N` 关闭 curl 的缓冲，才能看到逐字输出。若要一并拿到 token 用量，加上 `stream_options.include_usage`：

```bash
curl -N -X POST http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model":"chat-default",
    "messages":[{"role":"user","content":"你好，请用一句话介绍 Go"}],
    "stream":true,
    "stream_options":{"include_usage":true}
  }'
```

预期输出（每帧之间是空行分隔）：

```
data: {"id":"chatcmpl-1730000000000000000","object":"chat.completion.chunk","created":1730000000,"model":"chat-default","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-1730000000000000000","object":"chat.completion.chunk","created":1730000000,"model":"chat-default","choices":[{"index":0,"delta":{"content":"Go"},"finish_reason":null}]}

data: {"id":"chatcmpl-1730000000000000000","object":"chat.completion.chunk","created":1730000000,"model":"chat-default","choices":[{"index":0,"delta":{"content":" 是"},"finish_reason":null}]}

...

data: {"id":"chatcmpl-1730000000000000000","object":"chat.completion.chunk","created":1730000000,"model":"chat-default","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"chatcmpl-1730000000000000000","object":"chat.completion.chunk","created":1730000000,"model":"chat-default","choices":[],"usage":{"prompt_tokens":18,"completion_tokens":26,"total_tokens":44}}

data: [DONE]
```

> 用量帧的特征是 `choices` 为空数组。不传 `stream_options.include_usage` 时不会出现该帧。

### 8. 运行多实例（可选）

```bash
INSTANCE_NAME=chat-a go run ./cmd/chat_service -conf ./configs
INSTANCE_NAME=chat-b go run ./cmd/chat_service -conf ./configs-instance-2
```

Consul 中会出现两个 `chat_service` 实例，gateway 会在它们之间分发请求。调用 Greeter 接口时，响应里的 `from chat-a` / `from chat-b` 可以看出本次命中了哪个实例。

---

## API

完整字段说明与验收标准见 **[docs/api/chat-completion.md](docs/api/chat-completion.md)**。

| 项目 | 值 |
| --- | --- |
| 方法 | `POST` |
| 路径 | `/v1/chat/completions` |
| Content-Type | `application/json` |

### 响应模式

| `stream` | 响应类型 | 说明 |
| --- | --- | --- |
| `false`（默认） | `application/json` | 一次性返回完整结果，处理时限 70s |
| `true` | `text/event-stream` | 按块推送增量内容，以 `data: [DONE]` 结束，处理时限 300s |

流式响应遵循 OpenAI 的 SSE 约定：

- `object` 为 `chat.completion.chunk`，同一流的 `id` 与 `created` 保持一致
- 首帧只带 `delta.role = "assistant"`，之后逐帧推送 `delta.content`
- 终止帧带 `finish_reason`（如 `stop`）
- 若开启 `stream_options.include_usage`，在 `[DONE]` 之前额外返回一帧 `choices: []` 的用量事件
- 正常结束发送 `data: [DONE]`；**中途出错则发送 SSE 错误事件并终止，不再发送 `[DONE]`**

**错误响应**（统一结构 `{"error":{"message","type","param","code"}}`）

| HTTP | 场景 | `code` |
| --- | --- | --- |
| `400` | 请求体非法 / `model` 缺失 / `messages` 为空 / 角色不支持 | `invalid_request`、`invalid_role` |
| `500` | 服务端 ResponseWriter 不支持 Flush | `stream_unsupported` |
| `502` | 下游模型返回异常响应，或流式请求无法建立 | `upstream_error` |
| `503` | 模型服务不可用或熔断已打开 | `service_unavailable` |
| `504` | 模型调用超时（非流式，处理时限 70s） | `timeout` |

> 流式请求一旦开始返回 `200`，就无法再改写 HTTP 状态码，后续错误只能通过 SSE 错误事件表达，
> 其 `code` 取值与上表一致（`timeout` / `service_unavailable` / `upstream_error`）。

---

## 开发进度

**已完成**

- [x] gateway ↔ chat_service 双进程骨架，Wire 依赖注入
- [x] Consul 服务注册与发现，支持同机多实例
- [x] `POST /v1/chat/completions` 请求解析与参数校验
- [x] OpenAI 兼容的成功 / 错误 JSON 契约
- [x] 下游异常到 HTTP 状态码的映射（502 / 503 / 504）
- [x] 客户端熔断与退避重试
- [x] 跨进程 metadata 与 `request_id` 透传
- [x] **接入通义千问真实模型**，替换模拟回复
- [x] **token 用量取自模型返回的真实统计**：非流式直接返回，流式经 `include_usage`
- [x] 模型能力抽象为 `biz.ModelProvider` 接口，业务层与厂商解耦
- [x] **SSE 流式回答**：`StreamChatCompletion` 服务端流式 RPC + OpenAI 兼容 `text/event-stream`
- [x] **流式 token 用量透出**：支持 `stream_options.include_usage`，按需下发用量帧
- [x] **超时链路重构**：非流式 / 流式分别设定，内层短于外层
- [x] **流式中途错误细分**：区分超时、下游不可用与一般中断
- [x] **端到端集成测试**：覆盖超过旧传输层限制（1s / 2s）的慢响应

**待办**

- [ ] 多模型路由：按请求参数分发到不同厂商 / 模型，取代当前写死的 `chat-default` 判断
- [ ] 模型调用重试与降级策略（当前仅 gRPC 层有重试）
- [ ] Agent 编排能力
- [ ] 数据库 / Redis 实际接入，配置外部化
- [ ] CI 接入（当前测试需手动执行）

---

## 已知限制

- **逻辑模型名只有 `chat-default` 一个**，传入其他值会被 Provider 拒绝。
- 不支持的模型名目前经过 gateway 的错误映射后返回 **`502 upstream_error`**，语义上更接近客户端参数错误（`400`），后续统一错误分类时修正。
- 响应 `model` 字段回显请求值，未透出上游真实模型名。
- 流式的 `include_usage` 只控制是否下发用量帧；Provider 对上游始终请求用量。
- `configs-instance-2/config.yaml` 的 `server.grpc.timeout` 仍为 `1s`，第二实例跑慢请求会被截断，需与主实例对齐。
- 流式中途出错后不再写日志，`requestID` 辅助函数当前未被调用。

---

## 开发命令

| 命令 | 说明 |
| --- | --- |
| `make init` | 安装 protoc 相关工具链 |
| `make api` | 由 proto 生成 API 代码与 OpenAPI 描述 |
| `make config` | 由 proto 生成配置结构体 |
| `make generate` | `go generate` + `go mod tidy` |
| `make build` | 编译到 `bin/` |
| `make all` | 依次执行以上生成流程 |
| `go test ./...` | 运行测试 |

---

## Docker

```bash
docker build -t ai-gateway .

# 启动内网服务（默认），凭证通过 -e 注入
docker run --rm -p 8000:8000 -p 9000:9000 \
  -e DASHSCOPE_API_KEY="sk-你的key" \
  -e QWEN_BASE_URL="https://dashscope.aliyuncs.com/compatible-mode/v1" \
  -v /path/to/configs:/data/conf ai-gateway

# 启动对外网关
docker run --rm -p 8080:8080 ai-gateway ./gateway
```

> 注意：`configs/config.yaml` 中的监听地址是 `127.0.0.1`，仅适用于本机运行。
> 容器化部署时需要把地址改为 `0.0.0.0`，否则端口映射无法从宿主机访问。

---

## License

[MIT](LICENSE)
