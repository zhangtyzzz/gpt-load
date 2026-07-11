package utils

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

const maxContentEncodingLayers = 4

// ReadCompressedBodyBounded reads and decodes an HTTP representation without
// ever returning encoded bytes as if they were plaintext. The encoded input,
// every intermediate decoding layer, and the final output are independently
// bounded. Content codings are decoded in reverse application order per HTTP.
func ReadCompressedBodyBounded(reader io.Reader, contentEncoding string, encodedLimit, decodedLimit int64) ([]byte, error) {
	if reader == nil {
		return nil, nil
	}
	if encodedLimit <= 0 || decodedLimit <= 0 {
		return nil, fmt.Errorf("response body limits must be positive")
	}

	encodings, err := parseContentEncodings(contentEncoding)
	if err != nil {
		return nil, err
	}
	encoded, err := readAllBounded(reader, encodedLimit, "encoded response body")
	if err != nil {
		return nil, err
	}

	decoded := encoded
	for index := len(encodings) - 1; index >= 0; index-- {
		encoding := encodings[index]
		if encoding == "identity" {
			continue
		}
		decoded, err = decodeLayerBounded(decoded, encoding, decodedLimit)
		if err != nil {
			return nil, fmt.Errorf("decode %s response body: %w", encoding, err)
		}
	}
	if int64(len(decoded)) > decodedLimit {
		return nil, fmt.Errorf("decoded response body exceeds %d bytes", decodedLimit)
	}
	return decoded, nil
}

func parseContentEncodings(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxContentEncodingLayers {
		return nil, fmt.Errorf("content encoding has too many layers")
	}
	encodings := make([]string, 0, len(parts))
	for _, part := range parts {
		encoding := strings.ToLower(strings.Trim(part, " \t"))
		if encoding == "" {
			return nil, fmt.Errorf("content encoding contains an empty value")
		}
		switch encoding {
		case "identity", "gzip", "br", "deflate", "zstd":
			encodings = append(encodings, encoding)
		default:
			return nil, fmt.Errorf("unsupported content encoding")
		}
	}
	return encodings, nil
}

func decodeLayerBounded(encoded []byte, encoding string, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("decoded response body limit must be positive")
	}

	switch encoding {
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return readAllBounded(reader, limit, "decoded gzip body")
	case "br":
		return readAllBounded(brotli.NewReader(bytes.NewReader(encoded)), limit, "decoded brotli body")
	case "deflate":
		return decodeDeflateBounded(encoded, limit)
	case "zstd":
		memoryLimit := uint64(limit)
		if memoryLimit < 1<<20 {
			memoryLimit = 1 << 20
		}
		reader, err := zstd.NewReader(
			bytes.NewReader(encoded),
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderLowmem(true),
			zstd.WithDecoderMaxMemory(memoryLimit),
			zstd.WithDecoderMaxWindow(memoryLimit),
			zstd.WithDecodeBuffersBelow(0),
		)
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return readAllBounded(reader, limit, "decoded zstd body")
	default:
		return nil, fmt.Errorf("unsupported content encoding")
	}
}

// decodeDeflateBounded accepts the zlib-wrapped form defined for HTTP and the
// raw DEFLATE variant emitted by legacy servers. It never treats deflate as
// gzip and only falls back to raw DEFLATE when the zlib header is invalid.
func decodeDeflateBounded(encoded []byte, limit int64) ([]byte, error) {
	zlibReader, zlibErr := zlib.NewReader(bytes.NewReader(encoded))
	if zlibErr == nil {
		defer zlibReader.Close()
		return readAllBounded(zlibReader, limit, "decoded zlib body")
	}

	rawReader := flate.NewReader(bytes.NewReader(encoded))
	defer rawReader.Close()
	decoded, rawErr := readAllBounded(rawReader, limit, "decoded raw deflate body")
	if rawErr != nil {
		return nil, fmt.Errorf("zlib header: %v; raw deflate: %w", zlibErr, rawErr)
	}
	return decoded, nil
}

func readAllBounded(reader io.Reader, limit int64, description string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", description, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", description, limit)
	}
	return data, nil
}
