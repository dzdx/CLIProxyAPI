package auth

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

const testStreamBootstrapTimeoutMetadataKey = "stream_bootstrap_timeout"

type bootstrapTimeoutStreamExecutor struct {
	mu      sync.Mutex
	calls   int
	authIDs []string
}

func (e *bootstrapTimeoutStreamExecutor) Identifier() string { return "codex" }

func (e *bootstrapTimeoutStreamExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &Error{Code: "not_implemented", Message: "Execute not implemented"}
}

func (e *bootstrapTimeoutStreamExecutor) ExecuteStream(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	_ = req
	_ = opts

	authID := ""
	if auth != nil {
		authID = auth.ID
	}

	e.mu.Lock()
	e.calls++
	e.authIDs = append(e.authIDs, authID)
	e.mu.Unlock()

	if authID == "auth1" {
		ch := make(chan cliproxyexecutor.StreamChunk, 1)
		go func() {
			<-ctx.Done()
			ch <- cliproxyexecutor.StreamChunk{Err: ctx.Err()}
			close(ch)
		}()
		return &cliproxyexecutor.StreamResult{Chunks: ch}, nil
	}

	ch := make(chan cliproxyexecutor.StreamChunk, 1)
	ch <- cliproxyexecutor.StreamChunk{Payload: []byte("ok")}
	close(ch)
	return &cliproxyexecutor.StreamResult{
		Headers: http.Header{"X-Upstream-Attempt": {"2"}},
		Chunks:  ch,
	}, nil
}

func (e *bootstrapTimeoutStreamExecutor) Refresh(ctx context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e *bootstrapTimeoutStreamExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &Error{Code: "not_implemented", Message: "CountTokens not implemented"}
}

func (e *bootstrapTimeoutStreamExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, &Error{Code: "not_implemented", Message: "HttpRequest not implemented", HTTPStatus: http.StatusNotImplemented}
}

func (e *bootstrapTimeoutStreamExecutor) Calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func (e *bootstrapTimeoutStreamExecutor) AuthIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.authIDs))
	copy(out, e.authIDs)
	return out
}

func TestManagerExecuteStream_RetriesAfterBootstrapTimeoutBeforeFirstByte(t *testing.T) {
	executor := &bootstrapTimeoutStreamExecutor{}
	manager := NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)

	auth1 := &Auth{ID: "auth1", Provider: "codex", Status: StatusActive}
	if _, err := manager.Register(context.Background(), auth1); err != nil {
		t.Fatalf("manager.Register(auth1): %v", err)
	}
	auth2 := &Auth{ID: "auth2", Provider: "codex", Status: StatusActive}
	if _, err := manager.Register(context.Background(), auth2); err != nil {
		t.Fatalf("manager.Register(auth2): %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(auth1.ID, auth1.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	registry.GetGlobalRegistry().RegisterClient(auth2.ID, auth2.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth1.ID)
		registry.GetGlobalRegistry().UnregisterClient(auth2.ID)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	streamResult, err := manager.ExecuteStream(ctx, []string{"codex"}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{
		Metadata: map[string]any{testStreamBootstrapTimeoutMetadataKey: 10 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var payload []byte
	for chunk := range streamResult.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		payload = append(payload, chunk.Payload...)
	}

	if string(payload) != "ok" {
		t.Fatalf("payload = %q, want %q", string(payload), "ok")
	}
	if executor.Calls() != 2 {
		t.Fatalf("stream attempts = %d, want %d", executor.Calls(), 2)
	}
	gotAuthIDs := executor.AuthIDs()
	wantAuthIDs := []string{"auth1", "auth2"}
	if len(gotAuthIDs) != len(wantAuthIDs) {
		t.Fatalf("auth IDs = %v, want %v", gotAuthIDs, wantAuthIDs)
	}
	for i := range wantAuthIDs {
		if gotAuthIDs[i] != wantAuthIDs[i] {
			t.Fatalf("auth attempt %d = %q, want %q", i, gotAuthIDs[i], wantAuthIDs[i])
		}
	}
}
