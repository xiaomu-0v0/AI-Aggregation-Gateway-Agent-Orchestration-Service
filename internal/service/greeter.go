package service

import (
	"context"
	"fmt"
	"os"

	// "time"

	v1 "chat_service/api/chat_service/v1"
	"chat_service/internal/biz"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	kmetadata "github.com/go-kratos/kratos/v2/metadata"
)

// GreeterService is a greeter service.
type GreeterService struct {
	v1.UnimplementedGreeterServer

	uc *biz.GreeterUsecase
}

// NewGreeterService new a greeter service.
func NewGreeterService(uc *biz.GreeterUsecase) *GreeterService {
	return &GreeterService{uc: uc}
}

// SayHello implements helloworld.GreeterServer.
func (s *GreeterService) SayHello(ctx context.Context, in *v1.HelloRequest) (*v1.HelloReply, error) {
	if in.Name == "unavailable" {
		return nil, kerrors.ServiceUnavailable(
			"DEMO_UNAVAILABLE",
			"chat service temporarily unavailable",
		)
	}

	g, err := s.uc.CreateGreeter(ctx, &biz.Greeter{Hello: in.Name})
	if err != nil {
		return nil, err
	}

	md, _ := kmetadata.FromServerContext(ctx)

	requestIDs := md.Values("x-md-global-request-id")

	if len(requestIDs) > 0 {
		fmt.Println("Chat Service request_id:", requestIDs[0])
	}

	instanceName := os.Getenv("INSTANCE_NAME")
	if instanceName == "" {
		instanceName = "chat-unknow"
	}

	// select {
	// case <-time.After(
	// 	time.Second * 10,
	// ):
	// case <-ctx.Done():
	// 	return nil, ctx.Err()
	// }
	return &v1.HelloReply{
		Message: fmt.Sprintf(
			"hello %s from %s",
			g.Hello,
			instanceName,
		),
	}, nil
}
