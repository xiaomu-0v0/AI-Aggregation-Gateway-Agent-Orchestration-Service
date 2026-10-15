package server

import (
	middleware_metadata "chat_service/internal/gateway/middleware"
	"chat_service/internal/gateway/service"
	middleware_log "chat_service/internal/middleware"
	"os"

	v1 "chat_service/api/chat_service/v1"

	"github.com/go-kratos/kratos/v2/log"
	metamw "github.com/go-kratos/kratos/v2/middleware/metadata"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

// NewHTTPServer 创建 Gateway HTTP Server。
func NewHTTPServer(greeter *service.GreeterService, chatCompletion *service.ChatCompletionService) *khttp.Server {

	logger := log.NewStdLogger(os.Stdout) // 创建logger,标准输出，默认文本格式

	srv := khttp.NewServer(
		khttp.Address(":8080"),
		khttp.Middleware(
			middleware_log.Logger(logger),
			metamw.Server(),
			middleware_metadata.RequestID(),
			recovery.Recovery(),
		),
	)

	router := srv.Route("/")
	router.POST("/v1/chat/completions", chatCompletion.Handler)

	v1.RegisterGreeterHTTPServer(srv, greeter)

	return srv
}
