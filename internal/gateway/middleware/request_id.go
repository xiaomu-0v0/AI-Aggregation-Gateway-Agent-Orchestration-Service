package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	kmetadata "github.com/go-kratos/kratos/v2/metadata"
	"github.com/go-kratos/kratos/v2/middleware"
)

const RequestIDKey = "x-md-global-request-id"

// RequestID 如果请求已经有 request ID，就继续使用
// 如果没有，就在 Gateway 入口生成一个
func RequestID() middleware.Middleware {
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {

			// 读取当前请求已经携带的 metadata
			md, _ := kmetadata.FromServerContext(ctx)

			// 没有的话创建一份
			if md == nil {
				md = kmetadata.New()
			}

			// 看看上游是否已经给了 request ID
			requestIDs := md.Values(RequestIDKey)

			if len(requestIDs) == 0 || requestIDs[0] == "" {
				requestID := generateRequestID()
				md.Set(RequestIDKey, requestID)

				// 把新的 metadata 放回 context
				ctx = kmetadata.NewServerContext(ctx, md)
			}

			return next(ctx, req)
		}
	}
}

func generateRequestID() string {
	buf := make([]byte, 8)

	if _, err := rand.Read(buf); err != nil {
		// 这里只是学习实验，生产环境应该使用更完善的 ID 生成策略
		return "unknown"
	}

	return hex.EncodeToString(buf)
}
