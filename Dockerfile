FROM golang:1.22 AS builder

COPY . /src
WORKDIR /src

RUN GOPROXY=https://goproxy.cn make build

FROM debian:stable-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
		ca-certificates  \
        netbase \
        && rm -rf /var/lib/apt/lists/ \
        && apt-get autoremove -y && apt-get autoclean -y

COPY --from=builder /src/bin /app

WORKDIR /app

# chat_service: 内网 HTTP 8000 / gRPC 9000
EXPOSE 8000
EXPOSE 9000
# gateway: 对外 HTTP 8080
EXPOSE 8080

VOLUME /data/conf

# 默认启动内网 chat_service；如需启动对外网关：
#   docker run <image> ./gateway
CMD ["./chat_service", "-conf", "/data/conf"]
