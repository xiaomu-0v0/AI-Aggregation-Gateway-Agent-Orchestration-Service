//go:build wireinject
// +build wireinject

package main

// The build tag makes sure the stub is not built in the final build.

import (
	"chat_service/internal/biz"
	"chat_service/internal/conf"
	"chat_service/internal/data"
	"chat_service/internal/server"
	"chat_service/internal/service"

	serviceRegistry "chat_service/internal/registry"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
)

// wireApp init kratos application.
func wireApp(*conf.Server, *conf.Data, log.Logger) (*kratos.App, func(), error) {
	panic(wire.Build(server.ProviderSet, data.ProviderSet, biz.ProviderSet, service.ProviderSet, serviceRegistry.ProviderSet, newApp))
}
