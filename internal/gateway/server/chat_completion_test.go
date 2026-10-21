package server_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"chat_service/internal/biz"
	"chat_service/internal/conf"
	"chat_service/internal/data"
	gatewayclient "chat_service/internal/gateway/client"
	gatewayserver "chat_service/internal/gateway/server"
	gatewayservice "chat_service/internal/gateway/service"
	backendserver "chat_service/internal/server"
	backendservice "chat_service/internal/service"

	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/registry"
)

// Exercise the real HTTP -> discovery client -> gRPC -> Qwen path with a
// response slower than all three former 1s/2s transport limits.
func TestChatCompletionSlowUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2200 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"test\",\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"test","choices":[{"message":{"content":"hello"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(upstream.Close)
	t.Setenv("DASHSCOPE_API_KEY", "test-key")
	t.Setenv("QWEN_BASE_URL", upstream.URL)
	t.Setenv("QWEN_MODEL", "test-model")
	provider, err := data.NewQwenProvider()
	if err != nil {
		t.Fatal(err)
	}

	// Load the actual shipped configuration so a short server timeout is caught.
	c := config.New(config.WithSource(file.NewSource("../../../configs/config.yaml")))
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	var bootstrap conf.Bootstrap
	if err := c.Scan(&bootstrap); err != nil {
		t.Fatal(err)
	}
	bootstrap.Server.Grpc.Addr = "127.0.0.1:0"
	backend := backendserver.NewGRPCServer(bootstrap.Server, &backendservice.GreeterService{},
		backendservice.NewChatService(biz.NewChatUsecase(provider)), log.NewStdLogger(io.Discard))
	endpoint, err := backend.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if err := backend.Start(context.Background()); err != nil {
			t.Errorf("start backend: %v", err)
		}
	}()
	t.Cleanup(func() { _ = backend.Stop(context.Background()) })
	client, cleanup, err := gatewayclient.NewChatClient(&staticDiscovery{
		instances: []*registry.ServiceInstance{{
			ID: "test", Name: "chat_service", Endpoints: []string{endpoint.String()},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	gateway := httptest.NewServer(gatewayserver.NewHTTPServer(nil, gatewayservice.NewChatCompletionService(client)))
	t.Cleanup(gateway.Close)

	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			body := fmt.Sprintf(`{"model":"chat-default","messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"content":"hello"`) || strings.Contains(string(raw), `"error"`) {
				t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
			}
			if stream && !strings.Contains(string(raw), "data: [DONE]") {
				t.Fatalf("stream did not complete: %s", raw)
			}
		})
	}
}

type staticDiscovery struct {
	instances []*registry.ServiceInstance
}

func (d *staticDiscovery) GetService(context.Context, string) ([]*registry.ServiceInstance, error) {
	return d.instances, nil
}

func (d *staticDiscovery) Watch(ctx context.Context, _ string) (registry.Watcher, error) {
	ctx, cancel := context.WithCancel(ctx)
	return &staticWatcher{ctx: ctx, cancel: cancel, instances: d.instances}, nil
}

type staticWatcher struct {
	ctx       context.Context
	cancel    context.CancelFunc
	instances []*registry.ServiceInstance
	sent      bool
}

func (w *staticWatcher) Next() ([]*registry.ServiceInstance, error) {
	if !w.sent {
		w.sent = true
		return w.instances, nil
	}
	<-w.ctx.Done()
	return nil, w.ctx.Err()
}

func (w *staticWatcher) Stop() error {
	w.cancel()
	return nil
}
