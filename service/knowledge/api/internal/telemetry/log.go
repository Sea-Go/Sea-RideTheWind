package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// Metadata is set by the process entry before serving, never by business code.
type Metadata struct {
	Service     string `json:"service"`
	Environment string `json:"environment"`
	Version     string `json:"service_version"`
	Instance    string `json:"instance_id"`
}

// Writer adapts the public go-zero Writer contract. Its bounded queue drops on
// backpressure instead of blocking requests. The caller owns the output stream.
type Writer struct {
	meta     Metadata
	out      io.Writer
	queue    chan queuedRecord
	done     chan struct{}
	mu       sync.RWMutex
	closed   bool
	dropped  atomic.Uint64
	failures atomic.Uint64
}

type queuedRecord struct {
	raw     []byte
	barrier chan struct{}
}

func NewWriter(out io.Writer, meta Metadata, capacity int) (*Writer, error) {
	if out == nil || meta.Service == "" || meta.Environment == "" || meta.Version == "" || meta.Version == "latest" || meta.Instance == "" || capacity < 1 {
		return nil, errors.New("complete observability metadata and bounded writer are required")
	}
	w := &Writer{meta: meta, out: out, queue: make(chan queuedRecord, capacity), done: make(chan struct{})}
	go func() {
		defer close(w.done)
		for item := range w.queue {
			if item.barrier != nil {
				close(item.barrier)
				continue
			}
			n, err := w.out.Write(item.raw)
			if err != nil || n != len(item.raw) {
				w.failures.Add(1)
			}
		}
	}()
	return w, nil
}

// Install is called once, before any framework or business goroutine runs.
func (w *Writer) Install(level string) error {
	if err := logx.SetUp(logx.LogConf{Mode: "console", Encoding: "json", Level: level, Stat: false}); err != nil {
		return err
	}
	logx.SetWriter(w)
	switch level {
	case "debug":
		logx.SetLevel(logx.DebugLevel)
	case "error":
		logx.SetLevel(logx.ErrorLevel)
	default:
		logx.SetLevel(logx.InfoLevel)
	}
	return nil
}
func (w *Writer) write(level string, value any, fields ...logx.LogField) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		w.dropped.Add(1)
		return
	}
	message := fmt.Sprint(value)
	record := map[string]any{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "level": level, "service": w.meta.Service, "environment": w.meta.Environment, "service_version": w.meta.Version, "instance_id": w.meta.Instance, "component": "go-zero", "log_source": "framework", "event": "framework.log." + level, "message": message}
	// go-zero's authorization logger can append a complete HTTP request dump.
	// Keep the failure cause and correlation, but not headers or request bodies.
	if boundary := strings.Index(message, "\n=> "); boundary >= 0 {
		record["message"] = message[:boundary]
		record["request_dump_omitted"] = true
		record["original_message_bytes"] = len(message)
	}
	for _, field := range fields {
		key := field.Key
		switch key {
		case "trace":
			key = "trace_id"
		case "span":
			key = "span_id"
		case "caller":
			key = "code_location"
		}
		switch key {
		case "timestamp", "service", "environment", "service_version", "instance_id", "message":
			continue
		}
		if key == "level" {
			v, ok := field.Value.(string)
			if !ok || (v != "debug" && v != "info" && v != "warn" && v != "error") {
				continue
			}
		}
		record[key] = field.Value
	}
	if level == "error" && record["log_source"] == "framework" {
		if _, ok := record["error_code"]; !ok {
			record["error_code"] = "FRAMEWORK_ERROR"
			record["error_type"] = "go-zero"
			record["error_message"] = record["message"]
		}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		w.failures.Add(1)
		return
	}
	raw = append(raw, '\n')
	select {
	case w.queue <- queuedRecord{raw: raw}:
	default:
		w.dropped.Add(1)
	}
}
func (w *Writer) Debug(v any, f ...logx.LogField) { w.write("debug", v, f...) }
func (w *Writer) Info(v any, f ...logx.LogField)  { w.write("info", v, f...) }
func (w *Writer) Error(v any, f ...logx.LogField) { w.write("error", v, f...) }
func (w *Writer) Slow(v any, f ...logx.LogField)  { w.write("warn", v, f...) }
func (w *Writer) Stat(v any, f ...logx.LogField) {
	w.write("info", v, append(f, logx.Field("component", "runtime"), logx.Field("log_source", "runtime"), logx.Field("event", "runtime.statistics"))...)
}
func (w *Writer) Alert(v any) { w.write("error", v, logx.Field("event", "framework.alert")) }
func (w *Writer) Severe(v any) {
	w.write("error", v, logx.Field("event", "framework.severe"), logx.Field("exit_semantics", "fatal"))
}
func (w *Writer) Stack(v any) { w.write("error", v, logx.Field("event", "framework.stack")) }
func (w *Writer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return w.CloseContext(ctx)
}
func (w *Writer) Flush(ctx context.Context) error {
	barrier := make(chan struct{})
	w.mu.RLock()
	if w.closed {
		w.mu.RUnlock()
		return errors.New("log writer already closed")
	}
	select {
	case w.queue <- queuedRecord{barrier: barrier}:
		w.mu.RUnlock()
	case <-ctx.Done():
		w.mu.RUnlock()
		return ctx.Err()
	}
	select {
	case <-barrier:
		if count := w.failures.Load(); count > 0 {
			return fmt.Errorf("%d log sink writes failed", count)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (w *Writer) Dropped() uint64 { return w.dropped.Load() }
func (w *Writer) CloseContext(ctx context.Context) error {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.queue)
	}
	w.mu.Unlock()
	select {
	case <-w.done:
		if count := w.failures.Load(); count > 0 {
			return fmt.Errorf("%d log sink writes failed", count)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Process records lifecycle events through the same logx pipeline.
func Process(ctx context.Context, event, message, outcome string, started time.Time, err error, extra ...logx.LogField) {
	fields := []logx.LogField{logx.Field("component", "lifecycle"), logx.Field("log_source", "application"), logx.Field("event", event), logx.Field("outcome", outcome), logx.Field("duration_ms", float64(time.Since(started).Microseconds())/1000)}
	fields = append(fields, extra...)
	logger := logx.WithContext(ctx)
	if err != nil {
		fields = append(fields, logx.Field("error_code", "LIFECYCLE_ERROR"), logx.Field("error_type", fmt.Sprintf("%T", err)), logx.Field("error_message", err.Error()))
		logger.Errorw(message, fields...)
	} else {
		logger.Infow(message, fields...)
	}
}
func validIdentity(value string) bool {
	return value != "" && len(value) <= 200 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\t")
}
