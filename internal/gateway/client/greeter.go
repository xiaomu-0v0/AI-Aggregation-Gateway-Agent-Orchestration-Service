package client

import (
	"context"

	v1 "chat_service/api/chat_service/v1"

	"github.com/go-kratos/kratos/v2/middleware/circuitbreaker"
	metamw "github.com/go-kratos/kratos/v2/middleware/metadata"
	kregistry "github.com/go-kratos/kratos/v2/registry"
	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
)

// NewGreeterClient 创建 Chat Service 的 gRPC Client。
func NewGreeterClient(discovery kregistry.Discovery) (v1.GreeterClient, func(), error) {
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

	client := v1.NewGreeterClient(conn)

	cleanup := func() {
		_ = conn.Close()
	}

	return client, cleanup, nil
}
