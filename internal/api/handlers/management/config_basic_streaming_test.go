package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestStreamingFirstByteTimeoutManagement(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	h := &Handler{
		cfg: &config.Config{
			SDKConfig: config.SDKConfig{
				Streaming: config.StreamingConfig{FirstByteTimeoutSeconds: 15},
			},
		},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/streaming/first-byte-timeout-seconds", nil)

	h.GetStreamingFirstByteTimeout(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"first-byte-timeout-seconds":15`) {
		t.Fatalf("get body = %s, want first-byte-timeout-seconds 15", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/v0/management/streaming/first-byte-timeout-seconds", strings.NewReader(`{"value":20}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.PutStreamingFirstByteTimeout(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := h.cfg.Streaming.FirstByteTimeoutSeconds; got != 20 {
		t.Fatalf("first byte timeout = %d, want 20", got)
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/streaming/first-byte-timeout-seconds", strings.NewReader(`{"value":-5}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.PutStreamingFirstByteTimeout(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := h.cfg.Streaming.FirstByteTimeoutSeconds; got != 0 {
		t.Fatalf("first byte timeout = %d, want 0", got)
	}
}
