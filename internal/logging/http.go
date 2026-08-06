package logging

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status  int
	bytes   int
	body    *bytes.Buffer
	maxBody int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.body != nil && r.maxBody > 0 {
		remain := r.maxBody - r.body.Len()
		if remain > 0 {
			if len(p) <= remain {
				_, _ = r.body.Write(p)
			} else {
				_, _ = r.body.Write(p[:remain])
			}
		}
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// HTTPMiddleware logs each HTTP API call with leveled detail.
// INFO: method/path/status/duration plus request params and response body
// TRACE: start line with remote/ua
// ERROR: status >= 500
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = NewRequestID()
		}
		w.Header().Set("X-Request-ID", reqID)
		ctx := WithRequestID(r.Context(), reqID)
		r = r.WithContext(ctx)

		path := r.URL.Path
		remote := clientIP(r)
		skipBody := isHealthPath(path)

		Trace("http.api.start req_id=%s method=%s path=%s remote=%s ua=%q",
			reqID, r.Method, path, remote, r.UserAgent())

		reqPayload := ""
		if !skipBody {
			reqPayload = captureRequestBody(r)
			Info("http.api.req req_id=%s method=%s path=%s query=%q content_type=%q auth=%s params=%s",
				reqID, r.Method, path, r.URL.RawQuery, r.Header.Get("Content-Type"),
				authKind(r.Header.Get("Authorization")), reqPayload)
		} else if Enabled(LevelDebug) {
			Debug("http.api.detail req_id=%s query=%q content_length=%d content_type=%q auth=%s",
				reqID, r.URL.RawQuery, r.ContentLength, r.Header.Get("Content-Type"),
				authKind(r.Header.Get("Authorization")))
		}

		rec := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
			body:           &bytes.Buffer{},
			maxBody:        maxLogPayload * 4, // bytes budget before rune truncate in FormatPayload
		}
		if skipBody {
			rec.body = nil
			rec.maxBody = 0
		}

		next.ServeHTTP(rec, r)

		dur := time.Since(start)
		status := rec.status
		msg := "http.api.done req_id=%s method=%s path=%s status=%d bytes=%d dur_ms=%.2f remote=%s"

		switch {
		case status >= 500:
			Error(msg, reqID, r.Method, path, status, rec.bytes, float64(dur.Microseconds())/1000, remote)
		case skipBody:
			Trace(msg, reqID, r.Method, path, status, rec.bytes, float64(dur.Microseconds())/1000, remote)
		case status >= 400:
			Info(msg, reqID, r.Method, path, status, rec.bytes, float64(dur.Microseconds())/1000, remote)
		default:
			Info(msg, reqID, r.Method, path, status, rec.bytes, float64(dur.Microseconds())/1000, remote)
		}

		if !skipBody && rec.body != nil {
			Info("http.api.resp req_id=%s method=%s path=%s status=%d result=%s",
				reqID, r.Method, path, status, FormatPayload(rec.body.Bytes()))
		}
	})
}

func captureRequestBody(r *http.Request) string {
	query := r.URL.RawQuery
	if r.Body == nil || r.Body == http.NoBody {
		if query != "" {
			return "query=" + query
		}
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxReadBody+1))
	_ = r.Body.Close()
	if err != nil {
		r.Body = io.NopCloser(bytes.NewReader(nil))
		return "read_error=" + err.Error()
	}
	truncatedRead := len(body) > maxReadBody
	if truncatedRead {
		body = body[:maxReadBody]
	}
	// Restore full (capped) body for the handler.
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))

	payload := FormatPayload(body)
	if truncatedRead {
		if payload == "" {
			payload = "...(truncated)"
		} else if !strings.HasSuffix(payload, "...(truncated)") {
			payload += "...(truncated)"
		}
	}
	if query != "" {
		if payload == "" {
			return "query=" + query
		}
		return "query=" + query + " body=" + payload
	}
	if payload == "" {
		return ""
	}
	return payload
}

func isHealthPath(path string) bool {
	return path == "/healthz" || path == "/api/healthz"
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// authKind reports auth header presence without leaking the token.
func authKind(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return "none"
	}
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return "bearer"
	}
	return "other"
}
