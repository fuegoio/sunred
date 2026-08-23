// Package httplog provides an HTTP middleware that logs every incoming request.
package httplog

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const MaxBodyBytes = 4 * 1024

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		bodySnippet := captureBody(r)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("remote", r.RemoteAddr),
			slog.Int("status", rec.status),
			slog.Duration("dur", time.Since(start)),
		}
		if q := r.URL.RawQuery; q != "" {
			attrs = append(attrs, slog.String("query", q))
		}
		if bodySnippet != "" {
			attrs = append(attrs, slog.String("body", bodySnippet))
		}
		switch {
		case rec.status >= 500:
			slog.LogAttrs(r.Context(), slog.LevelError, "http", attrs...)
		case rec.status >= 400:
			slog.LogAttrs(r.Context(), slog.LevelWarn, "http", attrs...)
		default:
			slog.LogAttrs(r.Context(), slog.LevelInfo, "http", attrs...)
		}
	})
}

func captureBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, io.LimitReader(r.Body, MaxBodyBytes+1)); err != nil {
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf.Bytes()), r.Body))
		return ""
	}
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf.Bytes()), r.Body))

	snippet := buf.String()
	if len(snippet) > MaxBodyBytes {
		snippet = snippet[:MaxBodyBytes] + "...(truncated)"
	}
	return snippet
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.wroteHeader {
		return
	}
	s.status = code
	s.wroteHeader = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.status = http.StatusOK
		s.wroteHeader = true
	}
	return s.ResponseWriter.Write(b)
}
