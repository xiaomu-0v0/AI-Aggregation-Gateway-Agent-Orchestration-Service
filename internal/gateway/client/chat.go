package client

import (
	"context"
	"time"

	chatv1 "chat_service/api/chat/v1"

	"github.com/go-kratos/kratos/v2/middleware/circuitbreaker"
	metamw "github.com/go-kratos/kratos/v2/middleware/metadata"
	kregistry "github.com/go-kratos/kratos/v2/registry"
	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
)

// NewChatClient 创建内部 Chat Service 的 gRPC Client。
func NewChatClient(discovery kregistry.Discovery) (chatv1.ChatClient, func(), error) {
	conn, err := kgrpc.DialInsecure(
		context.Background(),
		kgrpc.WithEndpoint("discovery:///chat_service"),
		kgrpc.WithDiscovery(discovery),
		// 覆盖默认的 2 秒 unary 超时；调用方更短的期限仍然有效。
		kgrpc.WithTimeout(70*time.Second),
		kgrpc.WithMiddleware(
			metamw.Client(),
			circuitbreaker.Client(),
		),
	)
	if err != nil {
		return nil, nil, err
	}

	client := chatv1.NewChatClient(conn)

	cleanup := func() {
		_ = conn.Close()
	}

	return client, cleanup, nil
}
