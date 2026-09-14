package telemetry

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/rest/handler"
	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/zeromicro/go-zero/rest/router"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Router decorates go-zero's public router after registration has resolved a
// bounded route template. Framework TraceHandler is reused once, outside auth,
// timeout and request logging, so rejected JWT requests are covered as well.
type Router struct {
	httpx.Router
	runtime *Runtime
	service string
}

func NewRouter(runtime *Runtime, service string) *Router {
	r := &Router{Router: router.NewRouter(), runtime: runtime, service: service}
	r.SetNotFoundHandler(http.NotFoundHandler())
	r.SetNotAllowedHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusMethodNotAllowed) }))
	return r
}
func (r *Router) wrap(path, method string, h http.Handler) http.Handler {
	return handler.TraceHandler(r.service, path)(r.runtime.HTTP(path, method, h))
}
func (r *Router) Handle(method, path string, h http.Handler) error {
	if path == "/metrics" {
		return r.Router.Handle(method, path, h)
	}
	return r.Router.Handle(method, path, r.wrap(path, method, h))
}
func (r *Router) SetNotFoundHandler(h http.Handler) {
	if h == nil {
		h = http.NotFoundHandler()
	}
	r.Router.SetNotFoundHandler(r.wrap("unmatched", "other", h))
}
func (r *Router) SetNotAllowedHandler(h http.Handler) {
	if h == nil {
		h = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(405) })
	}
	r.Router.SetNotAllowedHandler(r.wrap("unmatched", "other", h))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(p)
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (r *Runtime) HTTP(route, method string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		effectiveMethod := method
		if method == "other" {
			effectiveMethod = boundedMethod(req.Method)
		}
		id := req.Header.Get("X-Request-ID")
		if !validIdentity(id) {
			id = uuid.NewString()
		}
		operation := req.Header.Get("X-Operation-ID")
		if !validIdentity(operation) {
			operation = ""
		}
		ctx := RequestContext(req.Context(), id, operation)
		started := time.Now()
		writer := &statusWriter{ResponseWriter: w}
		writer.Header().Set("X-Request-ID", id)
		fields := map[string]any{"component": "http", "log_source": "access", "request_id": id, "route": route, "method": effectiveMethod}
		emitStage(ctx, "http.request.started", "request started", "info", fields)
		next.ServeHTTP(writer, req.WithContext(ctx))
		status := writer.status
		if status == 0 {
			status = 200
		}
		outcome, level := "succeeded", "info"
		switch {
		case ctx.Err() == context.Canceled:
			outcome = "cancelled"
			level = "warn"
		case status == 504 || status == 408:
			outcome = "timeout"
			level = "warn"
		case status >= 500:
			outcome = "failed"
			level = "error"
		case status >= 400:
			outcome = "rejected"
			level = "warn"
		}
		elapsed := time.Since(started)
		fields["status"] = status
		fields["outcome"] = outcome
		fields["duration_ms"] = float64(elapsed.Microseconds()) / 1000
		if status >= 400 {
			fields["error_code"] = "HTTP_" + strconv.Itoa(status/100) + "XX"
			fields["error_type"] = "http.response"
			detail := http.StatusText(status)
			if detail == "" {
				detail = "HTTP status " + strconv.Itoa(status)
			}
			fields["error_message"] = detail
		}
		if operation := OperationID(ctx); operation != "" {
			fields["operation_id"] = operation
			trace.SpanFromContext(ctx).SetAttributes(attribute.String("operation.id", operation))
		}
		// The domain/response boundary owns detailed errors. Access logs carry only
		// terminal classification, never a second copy of the stack/error message.
		r.requests.WithLabelValues(route, effectiveMethod, strconv.Itoa(status/100)+"xx").Inc()
		r.httpDuration.WithLabelValues(route, effectiveMethod).Observe(elapsed.Seconds())
		emitStage(ctx, "http.request.completed", "request completed", level, fields)
	})
}

func boundedMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return method
	default:
		return "other"
	}
}
