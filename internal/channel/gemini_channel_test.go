package channel

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"gpt-load/internal/models"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestGeminiValidationErrorsNeverExposeQueryCredential(t *testing.T) {
	const secret = "gemini-validation-secret"
	upstream, err := url.Parse("https://generativelanguage.googleapis.com")
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}

	tests := []struct {
		name      string
		transport roundTripFunc
	}{
		{
			name: "transport error contains request URL",
			transport: func(request *http.Request) (*http.Response, error) {
				return nil, errors.New("dial failed for " + request.URL.String())
			},
		},
		{
			name: "upstream error body directly echoes key without a field name",
			transport: func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Incorrect credential ` + secret + `"}}`)),
					Request:    request,
				}, nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &GeminiChannel{BaseChannel: &BaseChannel{
				Name:       "gemini",
				Upstreams:  []UpstreamInfo{{URL: upstream, Weight: 1}},
				HTTPClient: &http.Client{Transport: test.transport},
				TestModel:  "gemini-test",
			}}
			_, validationErr := channel.ValidateKey(
				context.Background(),
				&models.APIKey{KeyValue: secret},
				&models.Group{},
			)
			if validationErr == nil {
				t.Fatal("ValidateKey returned no error")
			}
			if strings.Contains(validationErr.Error(), secret) {
				t.Fatalf("validation error leaked credential: %v", validationErr)
			}
		})
	}
}
