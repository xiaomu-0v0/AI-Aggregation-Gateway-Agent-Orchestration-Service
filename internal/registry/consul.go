package registry

import (
	"github.com/go-kratos/kratos/contrib/registry/consul/v2"
	kregistry "github.com/go-kratos/kratos/v2/registry"
	"github.com/google/wire"
	"github.com/hashicorp/consul/api"
)

// ProviderSet 告诉 Wire，本包可以提供哪些依赖
var ProviderSet = wire.NewSet(
	NewConsulRegistry,
	wire.Bind(new(kregistry.Registrar), new(*consul.Registry)),
	wire.Bind(new(kregistry.Discovery), new(*consul.Registry)),
)

// NewConsulRegistry 创建 Consul 注册中心客户端
func NewConsulRegistry() (*consul.Registry, error) {
	client, err := api.NewClient(&api.Config{
		Address: "127.0.0.1:8500",
	})
	if err != nil {
		return nil, err
	}

	return consul.New(client, consul.WithHealthCheck(false)), nil
}
