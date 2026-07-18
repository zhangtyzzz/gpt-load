package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"gpt-load/internal/channel"

	"github.com/gin-gonic/gin"
)

type failingBodyWriter struct {
	gin.ResponseWriter
	err error
}

type cancelOnEOFReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

type cancelOnDataReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

type cancelAfterTerminalReader struct {
	cancel context.CancelFunc
	stage  int
}

type cancelAfterTerminalWithErrorReader struct {
	cancel context.CancelFunc
	stage  int
}

type errorAfterTerminalReader struct {
	stage int
}

func (r *cancelOnEOFReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err == io.EOF {
		r.cancel()
	}
	return n, err
}

func (r *cancelOnDataReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.cancel()
	}
	return n, err
}

func (r *cancelAfterTerminalReader) Read(p []byte) (int, error) {
	switch r.stage {
	case 0:
		r.stage++
		return copy(p, "data: [DONE]\n\n"), nil
	case 1:
		r.stage++
		r.cancel()
		return copy(p, "\n"), nil
	default:
		return 0, io.EOF
	}
}

func (r *cancelAfterTerminalWithErrorReader) Read(p []byte) (int, error) {
	if r.stage == 0 {
		r.stage++
		return copy(p, "data: [DONE]\n\n"), nil
	}
	r.cancel()
	return 0, context.Canceled
}

func (r *errorAfterTerminalReader) Read(p []byte) (int, error) {
	if r.stage == 0 {
		r.stage++
		return copy(p, "data: [DONE]\n\n"), nil
	}
	return 0, io.ErrUnexpectedEOF
}

func (w *failingBodyWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

func TestRemoveHopByHopHeadersHonorsDynamicConnectionTokens(t *testing.T) {
	header := http.Header{
		"Connection":          []string{"X-Dynamic-One, x-dynamic-two"},
		"X-Dynamic-One":       []string{"one"},
		"X-Dynamic-Two":       []string{"two"},
		"Keep-Alive":          []string{"timeout=5"},
		"Proxy-Connection":    []string{"keep-alive"},
		"Proxy-Authorization": []string{"secret"},
		"X-End-To-End":        []string{"preserve"},
	}

	removeHopByHopHeaders(header)
	for _, name := range []string{"Connection", "X-Dynamic-One", "X-Dynamic-Two", "Keep-Alive", "Proxy-Connection", "Proxy-Authorization"} {
		if got := header.Get(name); got != "" {
			t.Fatalf("hop-by-hop header %s=%q remained", name, got)
		}
	}
	if got := header.Get("X-End-To-End"); got != "preserve" {
		t.Fatalf("end-to-end header = %q", got)
	}
}

func TestCopyResponseHeadersDoesNotMutateSource(t *testing.T) {
	source := http.Header{
		"Connection":     []string{"X-Internal"},
		"X-Internal":     []string{"hidden"},
		"X-End-To-End":   []string{"one", "two"},
		"Content-Type":   []string{"application/json"},
		"Content-Length": []string{"2"},
	}
	destination := make(http.Header)
	copyResponseHeaders(destination, source)
	if got := destination.Get("X-Internal"); got != "" {
		t.Fatalf("dynamic hop header was copied: %q", got)
	}
	if got := destination.Values("X-End-To-End"); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("end-to-end values = %#v", got)
	}
	if got := source.Get("X-Internal"); got != "hidden" {
		t.Fatalf("copy mutated source header: %q", got)
	}
}

func TestResponseHandlersReturnPreciseOutcomes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &ProxyServer{}

	t.Run("normal response completes", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/normal", nil)
		resp := &http.Response{Body: io.NopCloser(strings.NewReader("complete"))}

		result := server.handleNormalResponse(ctx, resp)
		if result.outcome != responseBodyCompleted || result.err != nil || recorder.Body.String() != "complete" {
			t.Fatalf("result=%#v body=%q", result, recorder.Body.String())
		}
	})

	t.Run("written SSE terminal event wins over cancellation at EOF", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		requestCtx, cancel := context.WithCancel(context.Background())
		ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(requestCtx)
		body := &cancelOnEOFReader{
			reader: iotest.OneByteReader(strings.NewReader("data: [DONE]\n\n")),
			cancel: cancel,
		}
		resp := &http.Response{Body: io.NopCloser(body)}

		result := server.handleStreamingResponse(ctx, resp)
		if result.outcome != responseBodyCompleted || result.err != nil {
			t.Fatalf("result=%#v body=%q", result, recorder.Body.String())
		}
		if recorder.Body.String() != "data: [DONE]\n\n" {
			t.Fatalf("body=%q", recorder.Body.String())
		}
	})

	t.Run("cancellation before SSE terminal write remains client cancellation", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		requestCtx, cancel := context.WithCancel(context.Background())
		ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(requestCtx)
		body := &cancelOnDataReader{
			reader: strings.NewReader("data: [DONE]\n\n"),
			cancel: cancel,
		}
		resp := &http.Response{Body: io.NopCloser(body)}

		result := server.handleStreamingResponse(ctx, resp)
		if result.outcome != responseBodyClientCancelled || !errors.Is(result.err, context.Canceled) {
			t.Fatalf("result=%#v body=%q", result, recorder.Body.String())
		}
		if recorder.Body.Len() != 0 {
			t.Fatalf("cancelled terminal event was written: %q", recorder.Body.String())
		}
	})

	t.Run("cancellation after SSE terminal write ignores trailing bytes", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		requestCtx, cancel := context.WithCancel(context.Background())
		ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(requestCtx)
		resp := &http.Response{Body: io.NopCloser(&cancelAfterTerminalReader{cancel: cancel})}

		result := server.handleStreamingResponse(ctx, resp)
		if result.outcome != responseBodyCompleted || result.err != nil {
			t.Fatalf("result=%#v body=%q", result, recorder.Body.String())
		}
		if recorder.Body.String() != "data: [DONE]\n\n" {
			t.Fatalf("body=%q", recorder.Body.String())
		}
	})

	t.Run("cancellation after SSE terminal write ignores a read error", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		requestCtx, cancel := context.WithCancel(context.Background())
		ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(requestCtx)
		resp := &http.Response{Body: io.NopCloser(&cancelAfterTerminalWithErrorReader{cancel: cancel})}

		result := server.handleStreamingResponse(ctx, resp)
		if result.outcome != responseBodyCompleted || result.err != nil {
			t.Fatalf("result=%#v body=%q", result, recorder.Body.String())
		}
		if recorder.Body.String() != "data: [DONE]\n\n" {
			t.Fatalf("body=%q", recorder.Body.String())
		}
	})

	t.Run("upstream truncation after SSE terminal write is completed", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
		resp := &http.Response{Body: io.NopCloser(&errorAfterTerminalReader{})}

		result := server.handleStreamingResponse(ctx, resp)
		if result.outcome != responseBodyCompleted || result.err != nil {
			t.Fatalf("result=%#v body=%q", result, recorder.Body.String())
		}
		if recorder.Body.String() != "data: [DONE]\n\n" {
			t.Fatalf("body=%q", recorder.Body.String())
		}
	})

	t.Run("non-stream cancellation at EOF remains client cancellation", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		requestCtx, cancel := context.WithCancel(context.Background())
		ctx.Request = httptest.NewRequest(http.MethodGet, "/normal", nil).WithContext(requestCtx)
		body := &cancelOnEOFReader{
			reader: strings.NewReader("complete"),
			cancel: cancel,
		}
		resp := &http.Response{Body: io.NopCloser(body)}

		result := server.handleNormalResponse(ctx, resp)
		if result.outcome != responseBodyClientCancelled || !errors.Is(result.err, context.Canceled) {
			t.Fatalf("result=%#v body=%q", result, recorder.Body.String())
		}
	})

	t.Run("upstream truncation is distinct", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/truncated", nil)
		body := io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(io.ErrUnexpectedEOF))
		resp := &http.Response{Body: io.NopCloser(body)}

		result := server.handleNormalResponse(ctx, resp)
		if result.outcome != responseBodyUpstreamTruncated || !errors.Is(result.err, io.ErrUnexpectedEOF) {
			t.Fatalf("result=%#v", result)
		}
		if recorder.Body.String() != "partial" {
			t.Fatalf("partial response was overwritten: %q", recorder.Body.String())
		}
	})

	t.Run("downstream write failure is distinct", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/write-failure", nil)
		writeErr := errors.New("downstream unavailable")
		ctx.Writer = &failingBodyWriter{ResponseWriter: ctx.Writer, err: writeErr}
		resp := &http.Response{Body: io.NopCloser(strings.NewReader("payload"))}

		result := server.handleNormalResponse(ctx, resp)
		if result.outcome != responseBodyDownstreamWriteFailed || !errors.Is(result.err, writeErr) {
			t.Fatalf("result=%#v", result)
		}
	})

	t.Run("cancelled context wins over write failure", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		requestCtx, cancel := context.WithCancel(context.Background())
		cancel()
		ctx.Request = httptest.NewRequest(http.MethodGet, "/cancelled", nil).WithContext(requestCtx)
		ctx.Writer = &failingBodyWriter{ResponseWriter: ctx.Writer, err: errors.New("broken pipe")}
		resp := &http.Response{Body: io.NopCloser(strings.NewReader("payload"))}

		result := server.handleNormalResponse(ctx, resp)
		if result.outcome != responseBodyClientCancelled || !errors.Is(result.err, context.Canceled) {
			t.Fatalf("result=%#v", result)
		}
	})
}

func TestLegacyStreamAndTransportCompatibility(t *testing.T) {
	legacy := &channel.OpenAIChannel{BaseChannel: &channel.BaseChannel{}}
	jsonResponse := &http.Response{Header: http.Header{"Content-Type": []string{"application/json"}}}
	eventResponse := &http.Response{Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	if !actualStreamForLog(true, jsonResponse, legacy) {
		t.Fatal("legacy request-side stream decision was lost")
	}
	if actualStreamForLog(false, eventResponse, legacy) {
		t.Fatal("legacy channel unexpectedly switched to response-side stream detection")
	}

	generic := &channel.GenericHTTPChannel{}
	if !actualStreamForLog(false, eventResponse, generic) {
		t.Fatal("explicit Generic response policy was not honored")
	}
	if got := transportFailureStatus(false); got != http.StatusInternalServerError {
		t.Fatalf("legacy transport status=%d, want 500", got)
	}
	if got := transportFailureStatus(true); got != http.StatusBadGateway {
		t.Fatalf("classified transport status=%d, want 502", got)
	}
}
