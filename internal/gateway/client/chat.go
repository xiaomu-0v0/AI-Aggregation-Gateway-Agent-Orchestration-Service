package client

import (
	"context"

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
