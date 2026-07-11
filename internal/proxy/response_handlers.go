package proxy

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type responseBodyOutcome string

const (
	responseBodyCompleted             responseBodyOutcome = "completed"
	responseBodyClientCancelled       responseBodyOutcome = "client_cancelled"
	responseBodyUpstreamTruncated     responseBodyOutcome = "upstream_truncated"
	responseBodyDownstreamWriteFailed responseBodyOutcome = "downstream_write_failed"
)

type responseBodyResult struct {
	outcome responseBodyOutcome
	err     error
}

func (r responseBodyResult) completed() bool {
	return r.outcome == responseBodyCompleted
}

func (ps *ProxyServer) handleStreamingResponse(c *gin.Context, resp *http.Response) responseBodyResult {
	if c.Writer.Header().Get("Content-Type") == "" {
		c.Header("Content-Type", "text/event-stream")
	}
	if c.Writer.Header().Get("Cache-Control") == "" {
		c.Header("Cache-Control", "no-cache")
	}
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		logrus.Error("Streaming unsupported by the writer, falling back to normal response")
		return ps.handleNormalResponse(c, resp)
	}

	return copyResponseBody(c, resp.Body, flusher)
}

// handleFlushedResponse preserves ordinary response metadata while forcing
// incremental writes. It is used by an explicit stream_mode=always choice even
// when the upstream content type is not SSE.
func (ps *ProxyServer) handleFlushedResponse(c *gin.Context, resp *http.Response) responseBodyResult {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return ps.handleNormalResponse(c, resp)
	}
	return copyResponseBody(c, resp.Body, flusher)
}

func copyResponseBody(c *gin.Context, body io.Reader, flusher http.Flusher) responseBodyResult {
	buf := make([]byte, 4*1024)
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			written, writeErr := c.Writer.Write(buf[:n])
			if writeErr == nil && written != n {
				writeErr = io.ErrShortWrite
			}
			if writeErr != nil {
				return classifyResponseWriteError(c, writeErr)
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			if requestContextError(c) != nil {
				return responseBodyResult{outcome: responseBodyClientCancelled, err: requestContextError(c)}
			}
			return responseBodyResult{outcome: responseBodyCompleted}
		}
		if readErr != nil {
			return classifyResponseReadError(c, readErr)
		}
	}
}

func classifyResponseReadError(c *gin.Context, err error) responseBodyResult {
	if requestContextError(c) != nil {
		return responseBodyResult{outcome: responseBodyClientCancelled, err: requestContextError(c)}
	}
	return responseBodyResult{outcome: responseBodyUpstreamTruncated, err: err}
}

func classifyResponseWriteError(c *gin.Context, err error) responseBodyResult {
	if requestContextError(c) != nil {
		return responseBodyResult{outcome: responseBodyClientCancelled, err: requestContextError(c)}
	}
	return responseBodyResult{outcome: responseBodyDownstreamWriteFailed, err: err}
}

func requestContextError(c *gin.Context) error {
	if c == nil || c.Request == nil {
		return nil
	}
	return c.Request.Context().Err()
}

func isEventStreamResponse(resp *http.Response) bool {
	return resp != nil && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
}

func copyResponseHeaders(destination, source http.Header) {
	cleaned := source.Clone()
	removeHopByHopHeaders(cleaned)
	for name, values := range cleaned {
		canonical := http.CanonicalHeaderKey(name)
		destination.Del(canonical)
		for _, value := range values {
			destination.Add(canonical, value)
		}
	}
}

// removeHopByHopHeaders applies the RFC Connection-token rule in addition to
// the fixed hop-by-hop header set. It must run before any request/response
// policy inspects headers, not only while copying the final response.
func removeHopByHopHeaders(header http.Header) {
	blocked := map[string]struct{}{
		"Connection":          {},
		"Keep-Alive":          {},
		"Proxy-Authenticate":  {},
		"Proxy-Authorization": {},
		"Proxy-Connection":    {},
		"Te":                  {},
		"Trailer":             {},
		"Transfer-Encoding":   {},
		"Upgrade":             {},
	}
	for _, connectionValue := range header.Values("Connection") {
		for _, name := range strings.Split(connectionValue, ",") {
			if canonical := http.CanonicalHeaderKey(strings.TrimSpace(name)); canonical != "" {
				blocked[canonical] = struct{}{}
			}
		}
	}
	for name := range blocked {
		header.Del(name)
	}
}

// removeRepresentationIntegrityHeaders removes validators calculated over a
// representation that the proxy decoded or rewrote. Forwarding them after a
// body change would falsely attest to bytes the client did not receive.
func removeRepresentationIntegrityHeaders(header http.Header) {
	for _, name := range []string{"ETag", "Content-MD5", "Digest", "Content-Digest", "Repr-Digest"} {
		header.Del(name)
	}
}

func (ps *ProxyServer) handleNormalResponse(c *gin.Context, resp *http.Response) responseBodyResult {
	return copyResponseBody(c, resp.Body, nil)
}
