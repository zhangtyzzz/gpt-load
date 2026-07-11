package proxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"gpt-load/internal/channel"
	"gpt-load/internal/errorpolicy"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

func TestShouldRetryClassifiedAttemptRequiresClassifierAndPolicyApproval(t *testing.T) {
	classification := channel.ResponseClassification{AllowRetry: true}
	retryDecision := errorpolicy.Decision{OnRequest: errorpolicy.RequestActionRetryOtherKey}
	returnDecision := errorpolicy.Decision{OnRequest: errorpolicy.RequestActionReturn}

	if !shouldRetryClassifiedAttempt(classification, retryDecision, 0, 1) {
		t.Fatal("retry should be allowed when both the channel and error policy approve it")
	}
	if shouldRetryClassifiedAttempt(classification, returnDecision, 0, 1) {
		t.Fatal("explicit return policy must prevent a classified transport retry")
	}
	if shouldRetryClassifiedAttempt(channel.ResponseClassification{}, retryDecision, 0, 1) {
		t.Fatal("error policy must not override a channel retry guard")
	}
	if shouldRetryClassifiedAttempt(classification, retryDecision, 1, 1) {
		t.Fatal("retry limit must still be enforced")
	}
}

func TestCooldownDurationUsesRetryAfterSeconds(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "3600")

	got := cooldownDuration(cooldownDecision(45), resp)
	if got != time.Hour {
		t.Fatalf("cooldownDuration = %v, want 1h", got)
	}
}

func TestReadUpstreamErrorBodyBoundedDecodesAndSanitizes(t *testing.T) {
	const secret = "sk-exact-compressed-upstream-key"
	payload := []byte(`{"error":"echo ` + secret + `"}`)
	gzipBody := encodeProxyCompressionTestBody(t, "gzip", payload)
	for _, tc := range []struct {
		name      string
		encodings []string
		body      []byte
	}{
		{name: "gzip mixed case", encodings: []string{" \tGZip "}, body: gzipBody},
		{name: "brotli", encodings: []string{"br"}, body: encodeProxyCompressionTestBody(t, "br", payload)},
		{name: "zlib deflate", encodings: []string{"deflate"}, body: encodeProxyCompressionTestBody(t, "zlib", payload)},
		{name: "raw deflate", encodings: []string{"deflate"}, body: encodeProxyCompressionTestBody(t, "deflate", payload)},
		{name: "zstd", encodings: []string{"zstd"}, body: encodeProxyCompressionTestBody(t, "zstd", payload)},
		{name: "stacked multi-value", encodings: []string{" GZip ", "\tBr "}, body: encodeProxyCompressionTestBody(t, "br", gzipBody)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				Header: http.Header{
					"Content-Encoding": tc.encodings,
					"Content-Length":   []string{"999"},
					"ETag":             []string{`"encoded"`},
					"Content-MD5":      []string{"legacy-digest"},
					"Digest":           []string{"sha-256=:abc:"},
					"Content-Digest":   []string{"sha-256=:abc:"},
				},
				Body:          io.NopCloser(bytes.NewReader(tc.body)),
				ContentLength: int64(len(tc.body)),
			}
			body, err := readUpstreamErrorBodyBounded(resp, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(body, payload) {
				t.Fatalf("decoded body = %q", body)
			}
			safeBody, parsed := sanitizeUpstreamError(body, secret)
			if strings.Contains(safeBody, secret) || strings.Contains(parsed, secret) {
				t.Fatalf("sanitized downstream error leaked key: body=%q parsed=%q", safeBody, parsed)
			}
			if resp.Header.Get("Content-Encoding") != "" || resp.Header.Get("Content-Length") != "" || resp.ContentLength != -1 {
				t.Fatalf("decoded response retained encoding metadata: header=%#v length=%d", resp.Header, resp.ContentLength)
			}
			for _, name := range []string{"ETag", "Content-MD5", "Digest", "Content-Digest"} {
				if got := resp.Header.Get(name); got != "" {
					t.Fatalf("decoded response retained %s=%q", name, got)
				}
			}
		})
	}
}

func TestReadUpstreamErrorBodyBoundedFailsClosed(t *testing.T) {
	const secret = "sk-compressed-fail-closed"
	for _, tc := range []struct {
		name     string
		encoding string
		body     []byte
	}{
		{name: "unsupported", encoding: "compress", body: []byte(secret)},
		{name: "malformed gzip", encoding: "gzip", body: []byte("not-gzip-" + secret)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				Header:        http.Header{"Content-Encoding": []string{tc.encoding}, "Content-Length": []string{"99"}},
				Body:          io.NopCloser(bytes.NewReader(tc.body)),
				ContentLength: int64(len(tc.body)),
			}
			body, err := readUpstreamErrorBodyBounded(resp, 1024)
			if err == nil || body != nil {
				t.Fatalf("failed decode returned body=%q error=%v", body, err)
			}
			if strings.Contains(string(body), secret) || strings.Contains(err.Error(), secret) {
				t.Fatalf("failed decode leaked encoded secret: body=%q error=%v", body, err)
			}
			if resp.Header.Get("Content-Encoding") != "" || resp.Header.Get("Content-Length") != "" || resp.ContentLength != -1 {
				t.Fatalf("failed response retained encoding metadata: header=%#v length=%d", resp.Header, resp.ContentLength)
			}
		})
	}
}

func TestReadUpstreamErrorBodyBoundedLimitsDecodedBomb(t *testing.T) {
	compressed := encodeProxyCompressionTestBody(t, "gzip", []byte(strings.Repeat("A", 2<<20)))
	resp := &http.Response{
		Header: http.Header{"Content-Encoding": []string{"gzip"}},
		Body:   io.NopCloser(bytes.NewReader(compressed)),
	}
	body, err := readUpstreamErrorBodyBounded(resp, 1024)
	if err == nil || body != nil {
		t.Fatalf("gzip bomb returned body=%d error=%v", len(body), err)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("decoded response retained Content-Encoding %q", got)
	}
}

func TestCooldownDurationUsesRetryAfterDate(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", time.Now().Add(2*time.Minute).UTC().Format(http.TimeFormat))

	got := cooldownDuration(cooldownDecision(45), resp)
	if got < 110*time.Second || got > 121*time.Second {
		t.Fatalf("cooldownDuration = %v, want approximately 2m", got)
	}
}

func TestCooldownDurationCapsRetryAfter(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "9999999")

	got := cooldownDuration(cooldownDecision(45), resp)
	if got != maxRetryAfterCooldown {
		t.Fatalf("cooldownDuration = %v, want max retry-after cap", got)
	}
}

func TestCooldownDurationFallsBackToPolicyParamWhenRetryAfterInvalid(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "not-a-duration")

	got := cooldownDuration(cooldownDecision(45), resp)
	if got != 45*time.Second {
		t.Fatalf("cooldownDuration = %v, want 45s", got)
	}
}

func TestCooldownDurationUsesPolicyParam(t *testing.T) {
	got := cooldownDuration(cooldownDecision(45), nil)
	if got != 45*time.Second {
		t.Fatalf("cooldownDuration = %v, want 45s", got)
	}
}

func TestCooldownDurationFallsBackToDefault(t *testing.T) {
	got := cooldownDuration(cooldownDecision(0), nil)
	if got != time.Duration(errorpolicy.DefaultCooldownSeconds)*time.Second {
		t.Fatalf("cooldownDuration = %v, want default cooldown", got)
	}
}

func TestCooldownDurationIgnoresNonCooldownHealth(t *testing.T) {
	got := cooldownDuration(errorpolicy.Decision{
		OnRequest: errorpolicy.RequestActionRetryOtherKey,
		Health:    errorpolicy.HealthActionFailCountInc,
	}, nil)
	if got != 0 {
		t.Fatalf("cooldownDuration = %v, want 0", got)
	}
}

func cooldownDecision(seconds int) errorpolicy.Decision {
	return errorpolicy.Decision{
		OnRequest: errorpolicy.RequestActionRetryOtherKey,
		Health:    errorpolicy.HealthActionCooldown,
		Params:    errorpolicy.Params{CooldownSeconds: seconds},
	}
}

func encodeProxyCompressionTestBody(t *testing.T, encoding string, payload []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	switch encoding {
	case "gzip":
		writer := gzip.NewWriter(&buffer)
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	case "br":
		writer := brotli.NewWriter(&buffer)
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	case "zlib":
		writer := zlib.NewWriter(&buffer)
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	case "deflate":
		writer, err := flate.NewWriter(&buffer, flate.DefaultCompression)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	case "zstd":
		writer, err := zstd.NewWriter(&buffer, zstd.WithEncoderConcurrency(1))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unsupported test encoding %q", encoding)
	}
	return buffer.Bytes()
}
