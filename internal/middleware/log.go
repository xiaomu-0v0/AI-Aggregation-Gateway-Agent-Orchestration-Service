package middleware

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
)

func Logger(logger log.Logger) middleware.Middleware {

	return func(handler middleware.Handler) middleware.Handler {

		return func(ctx context.Context, req interface{}) (interface{}, error) {

			start := time.Now()

			reply, err := handler(ctx, req)

			duration := time.Since(start)

			log.NewHelper(logger).Infof(
				"request cost:%v error:%v",
				duration,
				err,
			)

			return reply, err
		}
	}
}
