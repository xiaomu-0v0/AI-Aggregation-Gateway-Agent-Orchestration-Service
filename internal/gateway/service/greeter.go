package service

import (
	"context"
	"fmt"
	"time"

	v1 "chat_service/api/chat_service/v1"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GreeterService 是 Gateway 的业务入口。
// 它不自己处理真正的 Greeter 业务，而是调用下游 Chat Service。
type GreeterService struct {
	v1.UnimplementedGreeterServer

	client v1.GreeterClient
}

// NewGreeterService 创建 Gateway GreeterService。
func NewGreeterService(client v1.GreeterClient) *GreeterService {
	return &GreeterService{
		client: client,
	}
}

// SayHello 接收 HTTP 转换过来的请求，然后调用 Chat Service。
func (s *GreeterService) SayHello(
	ctx context.Context,
	in *v1.HelloRequest,
) (*v1.HelloReply, error) {

	// ctx, cancel := context.WithTimeout(
	// 	ctx,
	// 	3*time.Second,
	// )

	// defer cancel()

	const maxAttempts = 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		reply, err := s.client.SayHello(ctx, in)
		if err == nil {
			return reply, nil
		}

		// 非临时错误，或者达到最大尝试次数，直接返回。
		if !isRetryable(err) || attempt == maxAttempts {
			return nil, err
		}

		fmt.Printf(
			"[gateway] attempt=%d error=%v\n",
			attempt,
			err,
		)

		delay := time.Duration(attempt) * 100 * time.Millisecond
		timer := time.NewTimer(delay)

		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
			// 等待结束，进入下一次尝试。
		}
	}

	return nil, fmt.Errorf("unreachable retry state")
}

func isRetryable(err error) bool {
	// 熔断器已经打开时，不要反复重试相同的请求。
	if kerrors.Reason(err) == "CIRCUITBREAKER" {
		return false
	}

	// 本实验只对 gRPC Unavailable 状态进行重试。
	return status.Code(err) == codes.Unavailable
}
