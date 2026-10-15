package main

import (
	"github.com/go-kratos/kratos/v2"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

func newApp(httpServer *khttp.Server) *kratos.App {
	return kratos.New(
		kratos.Name("gateway"),
		kratos.Server(
			httpServer,
		),
	)
}

func main() {
	app, cleanup, err := wireApp()
	if err != nil {
		panic(err)
	}

	defer cleanup()

	if err := app.Run(); err != nil {
		panic(err)
	}
}
