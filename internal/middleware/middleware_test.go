package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gpt-load/internal/types"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestLoggerRedactsCredentialQueriesWithoutMutatingRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var logs bytes.Buffer
	oldOutput := logrus.StandardLogger().Out
	oldFormatter := logrus.StandardLogger().Formatter
	oldLevel := logrus.GetLevel()
	logrus.SetOutput(&logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true, DisableColors: true})
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(oldOutput)
		logrus.SetFormatter(oldFormatter)
		logrus.SetLevel(oldLevel)
	})

	const keySecret = "query-key-secret"
	const tokenSecret = "access-token-secret"
	requestQuerySeenByHandler := ""

	router := gin.New()
	router.Use(Logger(types.LogConfig{}))
	router.GET("/api/integration/info", func(c *gin.Context) {
		requestQuerySeenByHandler = c.Request.URL.RawQuery
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/integration/info?key="+keySecret+"&access_token="+tokenSecret+"&model=gpt-4&page=2", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)

	output := logs.String()
	for _, secret := range []string{keySecret, tokenSecret} {
		if strings.Contains(output, secret) {
			t.Fatalf("access log leaked %q: %s", secret, output)
		}
		if !strings.Contains(requestQuerySeenByHandler, secret) {
			t.Fatalf("logger unexpectedly mutated request query; handler did not receive %q", secret)
		}
	}
	for _, safe := range []string{"model=gpt-4", "page=2"} {
		if !strings.Contains(output, safe) {
			t.Errorf("access log lost non-sensitive query %q: %s", safe, output)
		}
	}
}
