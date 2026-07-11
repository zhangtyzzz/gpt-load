package utils

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

func TestReadCompressedBodyBoundedSupportedEncodings(t *testing.T) {
	payload := []byte(`{"error":"sk-compression-test"}`)
	gzipBody := encodeCompressionTestBody(t, "gzip", payload)
	tests := []struct {
		name     string
		encoding string
		body     []byte
	}{
		{name: "identity", encoding: "", body: payload},
		{name: "gzip mixed case and whitespace", encoding: " \tGZip\t ", body: gzipBody},
		{name: "brotli", encoding: "br", body: encodeCompressionTestBody(t, "br", payload)},
		{name: "zlib deflate", encoding: "deflate", body: encodeCompressionTestBody(t, "zlib", payload)},
		{name: "raw deflate", encoding: "deflate", body: encodeCompressionTestBody(t, "deflate", payload)},
		{name: "zstd", encoding: "zstd", body: encodeCompressionTestBody(t, "zstd", payload)},
		{
			name:     "stacked gzip then brotli",
			encoding: " GZip ,\tBr ",
			body:     encodeCompressionTestBody(t, "br", gzipBody),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReadCompressedBodyBounded(bytes.NewReader(tc.body), tc.encoding, 1<<20, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("decoded body = %q, want %q", got, payload)
			}
		})
	}
}

func TestReadCompressedBodyBoundedFailsClosed(t *testing.T) {
	const secret = "sk-compressed-error-secret"
	for _, tc := range []struct {
		name     string
		encoding string
		body     []byte
	}{
		{name: "unsupported", encoding: "compress", body: []byte(secret)},
		{name: "unsupported token equals secret", encoding: secret, body: []byte("opaque")},
		{name: "malformed gzip", encoding: "gzip", body: []byte("not-gzip-" + secret)},
		{name: "malformed list", encoding: "gzip,,br", body: []byte(secret)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := ReadCompressedBodyBounded(bytes.NewReader(tc.body), tc.encoding, 1<<20, 1<<20)
			if err == nil {
				t.Fatal("expected decoding error")
			}
			if body != nil {
				t.Fatalf("failed decode returned encoded bytes: %q", body)
			}
			if bytes.Contains([]byte(err.Error()), []byte(secret)) {
				t.Fatalf("decoding error leaked encoded body: %v", err)
			}
		})
	}
}

func TestReadCompressedBodyBoundedLimitsInputAndDecodedOutput(t *testing.T) {
	bomb := encodeCompressionTestBody(t, "gzip", bytes.Repeat([]byte("A"), 2<<20))
	if body, err := ReadCompressedBodyBounded(bytes.NewReader(bomb), "gzip", 64<<10, 1024); err == nil || body != nil {
		t.Fatalf("decoded bomb returned body=%d error=%v", len(body), err)
	}

	counting := &compressionCountingReader{Reader: bytes.NewReader(bytes.Repeat([]byte("x"), 4096))}
	if body, err := ReadCompressedBodyBounded(counting, "identity", 1024, 4096); err == nil || body != nil {
		t.Fatalf("oversized encoded body returned body=%d error=%v", len(body), err)
	}
	if counting.BytesRead > 1025 {
		t.Fatalf("encoded reader consumed %d bytes, want at most 1025", counting.BytesRead)
	}
}

type compressionCountingReader struct {
	io.Reader
	BytesRead int
}

func (r *compressionCountingReader) Read(buffer []byte) (int, error) {
	count, err := r.Reader.Read(buffer)
	r.BytesRead += count
	return count, err
}

func encodeCompressionTestBody(t *testing.T, encoding string, payload []byte) []byte {
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
