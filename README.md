# AI 聚合网关 / Agent 编排服务

基于 [Go Kratos v2](https://github.com/go-kratos/kratos) 构建的 AI 聚合网关与 Agent 编排服务。

对外提供 **OpenAI 兼容**的 HTTP 对话接口，对内通过 gRPC + Consul 组网，支持同机多实例水平扩展。

> **当前进度**：对话接口骨架（非流式）已打通，参数校验、错误映射、服务发现、熔断与重试均已就位。
> 下游模型调用目前是**模拟实现**，尚未接入真实大模型，`usage` 中的 token 用量为占位值。

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
                                                        ▼
                                              模拟回复（待接入模型）

                  ┌──────────────────────────────────────────────┐
                  │   Consul  127.0.0.1:8500   服务注册与发现      │
                  └──────────────────────────────────────────────┘
```

**设计要点**

| 关注点 | 实现方式 |
| --- | --- |
| 内外协议隔离 | 对外是手写的 OpenAI 兼容 JSON，对内是 protobuf gRPC，二者在 gateway 显式转换 |
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
│   ├── biz/                  # 业务逻辑
│   ├── conf/                 # 配置结构（protobuf 定义）
│   ├── data/                 # 数据访问
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

### 3. 启动内网服务

```bash
go run ./cmd/chat_service -conf ./configs
```

### 4. 启动对外网关

```bash
go run ./cmd/gateway
```

### 5. 发起调用

```bash
curl -X POST http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"chat-default","messages":[{"role":"user","content":"你好"}],"stream":false}'
```

预期返回：

```json
{
  "id": "chatcmpl-1730000000000000000",
  "object": "chat.completion",
  "created": 1730000000,
  "model": "chat-default",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "模拟 AI 回复：我收到了你的问题：你好"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": { "prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0 }
}
```

> 当前为模拟回复，`usage` 是占位值，不代表真实用量。

### 6. 运行多实例（可选）

```bash
go run ./cmd/chat_service -conf ./configs-instance-2
```

Consul 中会出现两个 `chat_service` 实例，gateway 会在它们之间分发请求。

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
| `502` | 下游返回未知错误 | `upstream_error` |
| `503` | 下游不可用或熔断已打开 | `service_unavailable` |
| `504` | 下游调用超时（网关侧上限 15s） | `timeout` |

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

**待办**

- [ ] 接入真实大模型，替换 `internal/service/chat.go` 中的模拟回复
- [ ] 以模型返回的真实 token 统计替换占位值
- [ ] SSE 流式响应（当前 `stream=true` 返回明确的不支持错误）
- [ ] 模型路由与多厂商适配（对应「AI 聚合网关」目标）
- [ ] Agent 编排能力
- [ ] 数据库 / Redis 实际接入，配置外部化
- [ ] 单元测试与 CI

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

# 启动内网服务（默认）
docker run --rm -p 8000:8000 -p 9000:9000 \
  -v /path/to/configs:/data/conf ai-gateway

# 启动对外网关
docker run --rm -p 8080:8080 ai-gateway ./gateway
```

> 注意：`configs/config.yaml` 中的监听地址是 `127.0.0.1`，仅适用于本机运行。
> 容器化部署时需要把地址改为 `0.0.0.0`，否则端口映射无法从宿主机访问。

---

## License

[MIT](LICENSE)
