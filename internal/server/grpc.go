package server

import (
	v1 "chat_service/api/chat_service/v1"
	"chat_service/internal/conf"
	"chat_service/internal/middleware"
	"chat_service/internal/service"

	chatv1 "chat_service/api/chat/v1"

	"github.com/go-kratos/kratos/v2/log"
	metamw "github.com/go-kratos/kratos/v2/middleware/metadata"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/transport/grpc"
)

// NewGRPCServer new a gRPC server.
func NewGRPCServer(c *conf.Server, greeter *service.GreeterService, chat *service.ChatService, logger log.Logger) *grpc.Server {
	var opts = []grpc.ServerOption{
		grpc.Middleware(
			middleware.Logger(logger),
			metamw.Server(),
			recovery.Recovery(),
		),
	}
	if c.Grpc.Network != "" {
		opts = append(opts, grpc.Network(c.Grpc.Network))
	}
	if c.Grpc.Addr != "" {
		opts = append(opts, grpc.Address(c.Grpc.Addr))
	}
	if c.Grpc.Timeout != nil {
		opts = append(opts, grpc.Timeout(c.Grpc.Timeout.AsDuration()))
	}
	srv := grpc.NewServer(opts...)
	v1.RegisterGreeterServer(srv, greeter)
	chatv1.RegisterChatServer(srv, chat)
	return srv
}
