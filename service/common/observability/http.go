package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/rest"
	"go.opentelemetry.io/otel/attribute"
)

const (
	httpClientClosedRequest = 499
	httpTimeoutBody         = "Request Timeout"
)

type httpEnvelope struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func DisableNativeRestTimeout(conf *rest.RestConf) time.Duration {
	timeout := TimeoutFromMillis(conf.Timeout)
	conf.Middlewares.Timeout = false
	return timeout
}

func NewHTTPMiddleware(serviceName string, timeout, slowThreshold time.Duration) rest.Middleware {
	if slowThreshold <= 0 {
		slowThreshold = DefaultSlowThreshold
	}

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if shouldSkipHTTPTimeout(r) || timeout <= 0 {
				start := time.Now()
				rec := newObservabilityResponseWriter()
				next(rec, r)
				duration := time.Since(start)
				markHTTPResult(r.Context(), serviceName, r, rec.statusCode(), rec.bodyBytes(), duration, slowThreshold)
				rec.flushTo(w)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			req := r.WithContext(ctx)
			rec := newObservabilityResponseWriter()
			start := time.Now()
			done := make(chan struct{})
			panicChan := make(chan any, 1)

			go func() {
				defer func() {
					if p := recover(); p != nil {
						panicChan <- fmt.Sprintf("%+v\n\n%s", p, strings.TrimSpace(string(debug.Stack())))
					}
				}()
				next(rec, req)
				close(done)
			}()

			select {
			case p := <-panicChan:
				MarkSystemError(ctx, fmt.Errorf("%v", p), time.Since(start), httpAttrs(serviceName, req, http.StatusInternalServerError)...)
				panic(p)
			case <-done:
				duration := time.Since(start)
				markHTTPResult(ctx, serviceName, req, rec.statusCode(), rec.bodyBytes(), duration, slowThreshold)
				rec.flushTo(w)
			case <-ctx.Done():
				duration := time.Since(start)
				rec.markTimedOut()
				statusCode := http.StatusServiceUnavailable
				if errors.Is(ctx.Err(), context.Canceled) {
					statusCode = httpClientClosedRequest
				}
				MarkTimeout(ctx, ctx.Err(), duration, httpAttrs(serviceName, req, statusCode)...)
				http.Error(w, httpTimeoutBody, statusCode)
			}
		}
	}
}

func shouldSkipHTTPTimeout(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") ||
		strings.EqualFold(r.Header.Get("Accept"), "text/event-stream")
}

func markHTTPResult(ctx context.Context, serviceName string, r *http.Request, statusCode int, body []byte, duration, slowThreshold time.Duration) {
	attrs := httpAttrs(serviceName, r, statusCode)
	envelope := parseHTTPEnvelope(body)
	if envelope != nil && envelope.Code != 0 && envelope.Code != SuccessBizCode {
		MarkBusinessError(ctx, envelope.Code, envelope.Msg, duration, attrs...)
		return
	}

	if statusCode >= http.StatusBadRequest {
		MarkSystemError(ctx, fmt.Errorf("http status %d", statusCode), duration, attrs...)
		return
	}

	MarkDurationAndSlow(ctx, duration, slowThreshold, attrs...)
}

func parseHTTPEnvelope(body []byte) *httpEnvelope {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var envelope httpEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}
	if envelope.Code == 0 && envelope.Msg == "" {
		return nil
	}
	return &envelope
}

func httpAttrs(serviceName string, r *http.Request, statusCode int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("service.name", serviceName),
		attribute.String("http.route", r.URL.Path),
		attribute.String("http.method", r.Method),
		attribute.Int("http.status_code", statusCode),
	}
}

type observabilityResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int

	mu       sync.Mutex
	timedOut bool
}

func newObservabilityResponseWriter() *observabilityResponseWriter {
	return &observabilityResponseWriter{
		header: make(http.Header),
		status: http.StatusOK,
	}
}

func (w *observabilityResponseWriter) Header() http.Header {
	return w.header
}

func (w *observabilityResponseWriter) WriteHeader(statusCode int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timedOut {
		return
	}
	w.status = statusCode
}

func (w *observabilityResponseWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	return w.body.Write(data)
}

func (w *observabilityResponseWriter) statusCode() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

func (w *observabilityResponseWriter) bodyBytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.body.Bytes()...)
}

func (w *observabilityResponseWriter) markTimedOut() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timedOut = true
}

func (w *observabilityResponseWriter) flushTo(dst http.ResponseWriter) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for key, values := range w.header {
		for _, value := range values {
			dst.Header().Add(key, value)
		}
	}
	if w.status != http.StatusOK {
		dst.WriteHeader(w.status)
	}
	if w.body.Len() > 0 {
		_, _ = io.Copy(dst, &w.body)
	}
}
