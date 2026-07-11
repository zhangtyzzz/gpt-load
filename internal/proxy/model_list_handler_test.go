package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gpt-load/internal/channel"
	"gpt-load/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestHandleModelListResponseUsesBoundedDecoder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &channel.OpenAIChannel{BaseChannel: &channel.BaseChannel{}}
	group := &models.Group{}

	t.Run("brotli", func(t *testing.T) {
		payload := []byte(`{"data":[{"id":"model-a"}]}`)
		compressed := encodeProxyCompressionTestBody(t, "br", payload)
		resp := &http.Response{
			Header:        http.Header{"Content-Encoding": []string{" Br "}, "Content-Length": []string{"999"}},
			Body:          io.NopCloser(bytes.NewReader(compressed)),
			ContentLength: int64(len(compressed)),
		}
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)

		_, _ = (&ProxyServer{}).handleModelListResponse(ctx, resp, group, handler, nil)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "model-a") {
			t.Fatalf("model-list response status=%d body=%q", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("unsupported fails closed", func(t *testing.T) {
		const secret = "sk-model-list-compressed-secret"
		resp := &http.Response{
			Header:        http.Header{"Content-Encoding": []string{"compress"}},
			Body:          io.NopCloser(strings.NewReader(secret)),
			ContentLength: int64(len(secret)),
		}
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)

		_, _ = (&ProxyServer{}).handleModelListResponse(ctx, resp, group, handler, nil)
		if recorder.Code != http.StatusBadGateway {
			t.Fatalf("model-list status=%d body=%q", recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), secret) {
			t.Fatalf("model-list failure leaked encoded body: %q", recorder.Body.String())
		}
	})

	t.Run("encoding token equal to selected key is absent from logs and API", func(t *testing.T) {
		const exactKey = "gzip"
		var logs bytes.Buffer
		oldOutput := logrus.StandardLogger().Out
		oldFormatter := logrus.StandardLogger().Formatter
		logrus.SetOutput(&logs)
		logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true, DisableColors: true})
		t.Cleanup(func() {
			logrus.SetOutput(oldOutput)
			logrus.SetFormatter(oldFormatter)
		})

		resp := &http.Response{
			Header: http.Header{"Content-Encoding": []string{exactKey}},
			Body:   io.NopCloser(strings.NewReader("malformed")),
		}
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		_, _ = (&ProxyServer{}).handleModelListResponse(ctx, resp, group, handler, &models.APIKey{KeyValue: exactKey})

		if combined := logs.String() + recorder.Body.String(); strings.Contains(combined, exactKey) {
			t.Fatalf("content-encoding credential leaked to log or API: %q", combined)
		}
	})
}
