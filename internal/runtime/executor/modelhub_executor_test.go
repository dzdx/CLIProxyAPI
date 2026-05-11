package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestModelHubExecutorExecute_UsesAKQueryInsteadOfAuthorization(t *testing.T) {
	var gotAuth string
	var gotQuery string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		gotBody = append([]byte(nil), body...)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","created":1,"model":"ali-deepseek-v4-pro","choices":[{"index":0,"message":{"role":"assistant","content":"world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	exec := NewModelHubExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "modelhub",
		Attributes: map[string]string{
			"api_key":  "secret-ak",
			"base_url": server.URL + "/crawl?foo=bar",
		},
	}

	resp, err := exec.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-pro",
		Payload: []byte(`{"model":"deepseek-pro","messages":[{"role":"user","content":"hello"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization header = %q, want empty", gotAuth)
	}
	if !strings.Contains(gotQuery, "ak=secret-ak") || !strings.Contains(gotQuery, "foo=bar") {
		t.Fatalf("query = %q, want merged ak and existing query", gotQuery)
	}
	if got := gjson.GetBytes(gotBody, "messages.0.content").String(); got != "hello" {
		t.Fatalf("upstream user content = %q, want hello", got)
	}
	if got := gjson.GetBytes(resp.Payload, "choices.0.message.content").String(); got != "world" {
		t.Fatalf("response content = %q, want world", got)
	}
}

func TestModelHubExecutorPrepareRequest_AddsAKQuery(t *testing.T) {
	exec := NewModelHubExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{
			"api_key":           "secret-ak",
			"header:X-TT-LOGID": "trace-id",
		},
	}
	req, err := http.NewRequest(http.MethodPost, "https://example.com/crawl?x=1", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	if err := exec.PrepareRequest(req, auth); err != nil {
		t.Fatalf("PrepareRequest returned error: %v", err)
	}
	if got := req.URL.Query().Get("ak"); got != "secret-ak" {
		t.Fatalf("ak query = %q, want secret-ak", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization header = %q, want empty", got)
	}
	if got := req.Header.Get("X-TT-LOGID"); got != "trace-id" {
		t.Fatalf("X-TT-LOGID = %q, want trace-id", got)
	}
}
