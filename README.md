# AI 聚合网关 / Agent 编排服务

基于 [Go Kratos v2](https://github.com/go-kratos/kratos) 构建的 AI 聚合网关与 Agent 编排服务。

对外提供 **OpenAI 兼容**的 HTTP 对话接口，对内通过 gRPC + Consul 组网，支持同机多实例水平扩展。

> **当前进度**：非流式对话链路已全链路打通，并已接入**通义千问（DashScope）**真实模型，
> `usage` 中的 token 用量来自模型返回的真实统计。SSE 流式响应尚未实现。

---

## 架构

```
                     HTTP POST /v1/chat/completions
    客户端 ─────────────────────────────────────────────┐
                                                        ▼
                                            ┌───────────────────────┐
                                            │        gateway        │
                                            │   对外 HTTP  :8080     │
                                            │  OpenAI 兼容 JSON 契约 │
                                            └───────────┬───────────┘
                                                        │ gRPC
                                          服务发现 discovery:///chat_service
                                           熔断 · 元数据透传 · 超时控制
                                                        │
                                                        ▼
                                            ┌───────────────────────┐
                                            │     chat_service      │
                                            │   内网 gRPC  :9000     │
                                            │   内网 HTTP  :8000     │
                                            │   Chat / Greeter 服务  │
                                            └───────────┬───────────┘
                                                        │
                                            biz.ModelProvider 接口
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
| 逻辑模型名 | 对外统一暴露 `chat-default`，由 `QWEN_MODEL` 决定背后真实调用的模型，为后续多厂商路由留出扩展位 |
| 服务发现 | Consul；gateway 通过 `discovery:///chat_service` 解析下游实例 |
| 容错 | 客户端熔断（`circuitbreaker`）+ 对 `Unavailable` 的退避重试（最多 3 次，熔断打开时不再重试） |
| 可观测 | 统一日志中间件、`request_id` 透传、metadata 跨进程传递 |
| 多实例 | `chat_service` 以「主机名 + gRPC 端口」作为 Consul 实例 ID，同一台机器可跑多实例 |

---

## 目录结构

```
.
├── api/                      # protobuf 契约与生成代码
│   ├── chat/v1/              #   内部 Chat 服务（gateway → chat_service）
│   └── chat_service/v1/      #   Greeter 示例服务
├── cmd/
│   ├── gateway/              # 对外 HTTP 网关（Wire 依赖注入）
│   └── chat_service/         # 内网 gRPC 服务（Wire 依赖注入）
├── configs/                  # 主实例配置
├── configs-instance-2/       # 第二实例配置（多实例演示）
├── docs/api/                 # 接口对接文档
├── internal/
│   ├── biz/                  # 业务用例与模型抽象
│   │   ├── model.go          #   ModelProvider 接口 + ChatUsecase
│   │   └── greeter.go        #   Greeter 用例
│   ├── conf/                 # 配置结构（protobuf 定义）
│   ├── data/                 # 数据访问与模型实现
│   │   └── qwen.go           #   通义千问 Provider（DashScope HTTP 调用）
│   ├── gateway/              # 网关专属：client / middleware / server / service
│   ├── middleware/           # 通用日志中间件
│   ├── registry/             # Consul 注册中心接入
│   ├── server/               # HTTP / gRPC server 装配
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

### 6. 发起调用

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

### 7. 运行多实例（可选）

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

**错误响应**（统一结构 `{"error":{"message","type","param","code"}}`）

| HTTP | 场景 | `code` |
| --- | --- | --- |
| `400` | 请求体非法 / `model` 缺失 / `messages` 为空 / 角色不支持 / `stream=true` | `invalid_request`、`invalid_role`、`unsupported_value` |
| `502` | 下游模型返回异常响应 | `upstream_error` |
| `503` | 模型服务不可用或熔断已打开 | `service_unavailable` |
| `504` | 模型调用超时（网关侧上限 15s） | `timeout` |

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
- [x] **token 用量取自模型返回的真实统计**
- [x] 模型能力抽象为 `biz.ModelProvider` 接口，业务层与厂商解耦

**待办**

- [ ] 多模型路由：按请求参数分发到不同厂商 / 模型，取代当前写死的 `chat-default` 判断
- [ ] SSE 流式响应（当前 `stream=true` 返回明确的不支持错误）
- [ ] 模型调用重试与降级策略（当前仅 gRPC 层有重试）
- [ ] Agent 编排能力
- [ ] 数据库 / Redis 实际接入，配置外部化
- [ ] 单元测试与 CI

---

## 已知限制

- **逻辑模型名只有 `chat-default` 一个**，传入其他值会被 Provider 拒绝。
- 不支持的模型名目前经过 gateway 的错误映射后返回 **`502 upstream_error`**，语义上更接近客户端参数错误（`400`），后续统一错误分类时修正。
- 响应 `model` 字段回显请求值，未透出上游真实模型名。
- 网关侧硬编码 15s 调用超时，Provider 侧 HTTP 客户端为 60s，二者尚未统一到配置。

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
