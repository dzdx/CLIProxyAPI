package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	gin "github.com/gin-gonic/gin"
	proxyconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	"github.com/tidwall/gjson"
)

const (
	apiIntegrationProvider      = "mockopenai"
	apiIntegrationAliasModel    = "compat-model"
	apiIntegrationUpstreamModel = "upstream-model"
	apiIntegrationLocalAPIKey   = "test-key"
	apiIntegrationUpstreamKey   = "upstream-secret"
)

type capturedUpstreamRequest struct {
	mu      sync.Mutex
	path    string
	auth    string
	body    []byte
	method  string
	queries string
}

func (c *capturedUpstreamRequest) record(r *http.Request, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.path = r.URL.Path
	c.auth = r.Header.Get("Authorization")
	c.method = r.Method
	c.queries = r.URL.RawQuery
	c.body = append([]byte(nil), body...)
}

func (c *capturedUpstreamRequest) snapshot() (path, auth, method, queries string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.path, c.auth, c.method, c.queries, append([]byte(nil), c.body...)
}

type apiIntegrationHarness struct {
	server   *Server
	proxyURL string
	upstream *capturedUpstreamRequest
}

func newOpenAICompatIntegrationHarness(t *testing.T, upstreamHandler http.HandlerFunc) *apiIntegrationHarness {
	t.Helper()

	gin.SetMode(gin.TestMode)

	upstreamCapture := &capturedUpstreamRequest{}
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, errRead := io.ReadAll(r.Body)
		if errRead != nil {
			t.Fatalf("read upstream request body: %v", errRead)
		}
		upstreamCapture.record(r, body)
		upstreamHandler(w, r)
	}))
	t.Cleanup(upstreamServer.Close)

	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if errMkdir := os.MkdirAll(authDir, 0o700); errMkdir != nil {
		t.Fatalf("create auth dir: %v", errMkdir)
	}

	cfg := &proxyconfig.Config{
		SDKConfig: sdkconfig.SDKConfig{
			APIKeys: []string{apiIntegrationLocalAPIKey},
		},
		Port:                   0,
		AuthDir:                authDir,
		Debug:                  true,
		LoggingToFile:          false,
		UsageStatisticsEnabled: false,
		OpenAICompatibility: []proxyconfig.OpenAICompatibility{{
			Name: apiIntegrationProvider,
			Models: []proxyconfig.OpenAICompatibilityModel{{
				Name:  apiIntegrationUpstreamModel,
				Alias: apiIntegrationAliasModel,
			}},
		}},
	}

	authManager := coreauth.NewManager(nil, nil, nil)
	authManager.SetConfig(cfg)
	authManager.RegisterExecutor(runtimeexecutor.NewOpenAICompatExecutor(apiIntegrationProvider, cfg))

	auth := &coreauth.Auth{
		ID:       "auth-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")),
		Provider: apiIntegrationProvider,
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"api_key":      apiIntegrationUpstreamKey,
			"base_url":     upstreamServer.URL,
			"compat_name":  apiIntegrationProvider,
			"provider_key": apiIntegrationProvider,
		},
	}
	if _, errRegister := authManager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, apiIntegrationProvider, []*registry.ModelInfo{{
		ID:      apiIntegrationAliasModel,
		Object:  "model",
		Created: 1,
		OwnedBy: "openai",
	}})
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})

	accessManager := sdkaccess.NewManager()
	configPath := filepath.Join(tmpDir, "config.yaml")
	server := NewServer(cfg, authManager, accessManager, configPath)
	proxyServer := httptest.NewServer(server.engine)
	t.Cleanup(proxyServer.Close)

	return &apiIntegrationHarness{
		server:   server,
		proxyURL: proxyServer.URL,
		upstream: upstreamCapture,
	}
}

func newModelHubIntegrationHarness(t *testing.T, upstreamHandler http.HandlerFunc) *apiIntegrationHarness {
	t.Helper()

	gin.SetMode(gin.TestMode)

	upstreamCapture := &capturedUpstreamRequest{}
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, errRead := io.ReadAll(r.Body)
		if errRead != nil {
			t.Fatalf("read upstream request body: %v", errRead)
		}
		upstreamCapture.record(r, body)
		upstreamHandler(w, r)
	}))
	t.Cleanup(upstreamServer.Close)

	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if errMkdir := os.MkdirAll(authDir, 0o700); errMkdir != nil {
		t.Fatalf("create auth dir: %v", errMkdir)
	}

	cfg := &proxyconfig.Config{
		SDKConfig: sdkconfig.SDKConfig{
			APIKeys: []string{apiIntegrationLocalAPIKey},
		},
		Port:                   0,
		AuthDir:                authDir,
		Debug:                  true,
		LoggingToFile:          false,
		UsageStatisticsEnabled: false,
		ModelHubAPIKey: []proxyconfig.ModelHubKey{{
			APIKey:  apiIntegrationUpstreamKey,
			BaseURL: upstreamServer.URL + "/crawl?foo=bar",
			Models: []proxyconfig.ModelHubModel{{
				Name:  apiIntegrationUpstreamModel,
				Alias: apiIntegrationAliasModel,
			}},
		}},
	}

	authManager := coreauth.NewManager(nil, nil, nil)
	authManager.SetConfig(cfg)
	authManager.RegisterExecutor(runtimeexecutor.NewModelHubExecutor(cfg))

	auth := &coreauth.Auth{
		ID:       "auth-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")),
		Provider: "modelhub",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"api_key":      apiIntegrationUpstreamKey,
			"base_url":     upstreamServer.URL + "/crawl?foo=bar",
			"provider_key": "modelhub",
		},
	}
	if _, errRegister := authManager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, "modelhub", []*registry.ModelInfo{{
		ID:      apiIntegrationAliasModel,
		Object:  "model",
		Created: 1,
		OwnedBy: "modelhub",
	}})
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})

	accessManager := sdkaccess.NewManager()
	configPath := filepath.Join(tmpDir, "config.yaml")
	server := NewServer(cfg, authManager, accessManager, configPath)
	proxyServer := httptest.NewServer(server.engine)
	t.Cleanup(proxyServer.Close)

	return &apiIntegrationHarness{
		server:   server,
		proxyURL: proxyServer.URL,
		upstream: upstreamCapture,
	}
}

func TestAPIIntegration_HealthzAndModelsAuthorization(t *testing.T) {
	h := newOpenAICompatIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("upstream should not be called")
	})

	resp, errGet := http.Get(h.proxyURL + "/healthz")
	if errGet != nil {
		t.Fatalf("GET /healthz: %v", errGet)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		t.Fatalf("read healthz body: %v", errRead)
	}
	if got := gjson.GetBytes(body, "status").String(); got != "ok" {
		t.Fatalf("healthz status field = %q, want ok", got)
	}

	modelsReq, _ := http.NewRequest(http.MethodGet, h.proxyURL+"/v1/models", nil)
	modelsResp, errModels := http.DefaultClient.Do(modelsReq)
	if errModels != nil {
		t.Fatalf("GET /v1/models without auth: %v", errModels)
	}
	defer func() {
		_ = modelsResp.Body.Close()
	}()
	if modelsResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("models status without auth = %d, want %d", modelsResp.StatusCode, http.StatusUnauthorized)
	}

	modelsReq, _ = http.NewRequest(http.MethodGet, h.proxyURL+"/v1/models", nil)
	modelsReq.Header.Set("Authorization", "Bearer "+apiIntegrationLocalAPIKey)
	modelsResp, errModels = http.DefaultClient.Do(modelsReq)
	if errModels != nil {
		t.Fatalf("GET /v1/models with auth: %v", errModels)
	}
	defer func() {
		_ = modelsResp.Body.Close()
	}()
	if modelsResp.StatusCode != http.StatusOK {
		t.Fatalf("models status with auth = %d, want %d", modelsResp.StatusCode, http.StatusOK)
	}
	modelsBody, _ := io.ReadAll(modelsResp.Body)
	if !bytes.Contains(modelsBody, []byte(`"id":"`+apiIntegrationAliasModel+`"`)) {
		t.Fatalf("models body missing alias model %q: %s", apiIntegrationAliasModel, string(modelsBody))
	}
}

func TestAPIIntegration_ChatCompletionsNonStreaming_RoutesAliasToOpenAICompatUpstream(t *testing.T) {
	h := newOpenAICompatIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("upstream path = %q, want /chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"` + apiIntegrationUpstreamModel + `","choices":[{"index":0,"message":{"role":"assistant","content":"world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	})

	payload := `{"model":"` + apiIntegrationAliasModel + `","messages":[{"role":"user","content":"hello"}],"max_tokens":8}`
	respBody := doJSONRequest(t, http.MethodPost, h.proxyURL+"/v1/chat/completions", payload, http.StatusOK)

	if got := gjson.GetBytes(respBody, "choices.0.message.content").String(); got != "world" {
		t.Fatalf("assistant content = %q, want world; body=%s", got, string(respBody))
	}

	path, authHeader, method, _, upstreamBody := h.upstream.snapshot()
	if path != "/chat/completions" || method != http.MethodPost {
		t.Fatalf("upstream call = %s %s, want POST /chat/completions", method, path)
	}
	if authHeader != "Bearer "+apiIntegrationUpstreamKey {
		t.Fatalf("upstream auth header = %q, want Bearer %s", authHeader, apiIntegrationUpstreamKey)
	}
	if got := gjson.GetBytes(upstreamBody, "model").String(); got != apiIntegrationUpstreamModel {
		t.Fatalf("upstream model = %q, want %q; body=%s", got, apiIntegrationUpstreamModel, string(upstreamBody))
	}
	if got := gjson.GetBytes(upstreamBody, "messages.0.content").String(); got != "hello" {
		t.Fatalf("upstream user content = %q, want hello; body=%s", got, string(upstreamBody))
	}
}

func TestAPIIntegration_ChatCompletionsStreaming_ProxiesSSE(t *testing.T) {
	h := newOpenAICompatIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("upstream path = %q, want /chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected upstream response writer to support flush")
		}
		for _, chunk := range []string{
			`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"` + apiIntegrationUpstreamModel + `","choices":[{"index":0,"delta":{"role":"assistant","content":"hel"},"finish_reason":null}]}` + "\n\n",
			`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"` + apiIntegrationUpstreamModel + `","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":null}]}` + "\n\n",
			`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"` + apiIntegrationUpstreamModel + `","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		} {
			_, _ = io.WriteString(w, chunk)
			flusher.Flush()
		}
	})

	req, _ := http.NewRequest(http.MethodPost, h.proxyURL+"/v1/chat/completions", strings.NewReader(`{"model":"`+apiIntegrationAliasModel+`","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	req.Header.Set("Authorization", "Bearer "+apiIntegrationLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, errDo := http.DefaultClient.Do(req)
	if errDo != nil {
		t.Fatalf("stream request: %v", errDo)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("stream status = %d, want %d: %s", resp.StatusCode, http.StatusOK, string(body))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("stream content-type = %q, want text/event-stream", ct)
	}
	streamBody, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(streamBody, []byte(`"content":"hel"`)) || !bytes.Contains(streamBody, []byte(`"content":"lo"`)) {
		t.Fatalf("stream body missing streamed content: %s", string(streamBody))
	}
	if !bytes.Contains(streamBody, []byte(`[DONE]`)) {
		t.Fatalf("stream body missing [DONE]: %s", string(streamBody))
	}
}

func TestAPIIntegration_ChatCompletions_AcceptsResponsesStylePayload(t *testing.T) {
	h := newOpenAICompatIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_2","object":"chat.completion","created":1,"model":"` + apiIntegrationUpstreamModel + `","choices":[{"index":0,"message":{"role":"assistant","content":"yes"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`))
	})

	payload := `{"model":"` + apiIntegrationAliasModel + `","instructions":"Be terse","input":"Reply yes","max_output_tokens":12}`
	respBody := doJSONRequest(t, http.MethodPost, h.proxyURL+"/v1/chat/completions", payload, http.StatusOK)
	if got := gjson.GetBytes(respBody, "choices.0.message.content").String(); got != "yes" {
		t.Fatalf("assistant content = %q, want yes; body=%s", got, string(respBody))
	}

	_, _, _, _, upstreamBody := h.upstream.snapshot()
	if got := gjson.GetBytes(upstreamBody, "messages.0.role").String(); got != "system" {
		t.Fatalf("converted first message role = %q, want system; body=%s", got, string(upstreamBody))
	}
	if got := gjson.GetBytes(upstreamBody, "messages.0.content").String(); got != "Be terse" {
		t.Fatalf("converted instructions = %q, want Be terse; body=%s", got, string(upstreamBody))
	}
	if got := gjson.GetBytes(upstreamBody, "messages.1.role").String(); got != "user" {
		t.Fatalf("converted second message role = %q, want user; body=%s", got, string(upstreamBody))
	}
	if got := gjson.GetBytes(upstreamBody, "messages.1.content").String(); got != "Reply yes" {
		t.Fatalf("converted input = %q, want Reply yes; body=%s", got, string(upstreamBody))
	}
	if got := gjson.GetBytes(upstreamBody, "max_tokens").String(); got != "12" {
		t.Fatalf("converted max_tokens = %q, want 12; body=%s", got, string(upstreamBody))
	}
}

func TestAPIIntegration_ModelHub_ChatCompletionsNonStreaming_UsesAKQuery(t *testing.T) {
	h := newModelHubIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crawl" {
			t.Fatalf("upstream path = %q, want /crawl", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_3","object":"chat.completion","created":1,"model":"` + apiIntegrationUpstreamModel + `","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	})

	payload := `{"model":"` + apiIntegrationAliasModel + `","messages":[{"role":"user","content":"hello"}],"max_tokens":8}`
	respBody := doJSONRequest(t, http.MethodPost, h.proxyURL+"/v1/chat/completions", payload, http.StatusOK)
	if got := gjson.GetBytes(respBody, "choices.0.message.content").String(); got != "ok" {
		t.Fatalf("assistant content = %q, want ok; body=%s", got, string(respBody))
	}

	path, authHeader, method, query, upstreamBody := h.upstream.snapshot()
	if path != "/crawl" || method != http.MethodPost {
		t.Fatalf("upstream call = %s %s, want POST /crawl", method, path)
	}
	if authHeader != "" {
		t.Fatalf("upstream auth header = %q, want empty", authHeader)
	}
	if !strings.Contains(query, "foo=bar") || !strings.Contains(query, "ak="+apiIntegrationUpstreamKey) {
		t.Fatalf("upstream query = %q, want foo=bar and ak", query)
	}
	if got := gjson.GetBytes(upstreamBody, "model").String(); got != apiIntegrationUpstreamModel {
		t.Fatalf("upstream model = %q, want %q; body=%s", got, apiIntegrationUpstreamModel, string(upstreamBody))
	}
}

func doJSONRequest(t *testing.T, method, targetURL, payload string, wantStatus int) []byte {
	t.Helper()

	req, _ := http.NewRequest(method, targetURL, strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+apiIntegrationLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, errDo := http.DefaultClient.Do(req)
	if errDo != nil {
		t.Fatalf("%s %s: %v", method, targetURL, errDo)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		t.Fatalf("read response body: %v", errRead)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d: %s", method, targetURL, resp.StatusCode, wantStatus, string(body))
	}
	return body
}
