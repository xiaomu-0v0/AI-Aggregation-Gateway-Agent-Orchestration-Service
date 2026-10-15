//go:build wireinject
// +build wireinject

package main

import (
	gatewayclient "chat_service/internal/gateway/client"
	gatewayserver "chat_service/internal/gateway/server"
	gatewayservice "chat_service/internal/gateway/service"
	serviceRegistry "chat_service/internal/registry"

	"github.com/go-kratos/kratos/v2"
	"github.com/google/wire"
)

// wireApp 使用 Wire 自动组装 Gateway 所有依赖

func wireApp() (*kratos.App, func(), error) {
	panic(wire.Build(
		gatewayclient.NewGreeterClient,
		gatewayservice.NewGreeterService,
		gatewayserver.NewHTTPServer,
		serviceRegistry.ProviderSet,
		gatewayclient.NewChatClient,
		gatewayservice.NewChatCompletionService,
		newApp,
	))
}
