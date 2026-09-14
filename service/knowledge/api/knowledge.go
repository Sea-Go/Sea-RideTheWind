package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/proc"

	"sea-try-go/service/knowledge/api/internal/config"
	"sea-try-go/service/knowledge/api/internal/handler"
	"sea-try-go/service/knowledge/api/internal/mqs"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/telemetry"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

func main() {
	os.Exit(mainExit())
}

func mainExit() int {
	configFile := flag.String("f", "service/knowledge/api/etc/knowledge-api.yaml", "configuration path")
	flag.Parse()
	var c config.Config
	if err := conf.Load(*configFile, &c, conf.UseEnv()); err != nil {
		bootstrapFailure("knowledge.config.failed", err)
		return 1
	}
	if err := run(c); err != nil {
		return 1
	}
	return 0
}

func buildRevision() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
	}
	return ""
}

func bootstrapFailure(event string, err error) {
	version := os.Getenv("KNOWLEDGE_SERVICE_VERSION")
	if version == "" {
		version = buildRevision()
	}
	if version == "" {
		version = "unresolved"
	}
	writer, setupErr := telemetry.NewWriter(os.Stdout, telemetry.Metadata{Service: "ridethewind.knowledge", Environment: "startup", Version: version, Instance: uuid.NewString()}, 16)
	if setupErr != nil {
		return
	}
	if writer.Install("info") == nil {
		telemetry.Process(context.Background(), event, "knowledge startup failed", "failed", time.Now(), err)
		logx.Reset()
	}
	flush, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = writer.CloseContext(flush)
}

func run(c config.Config) (result error) {
	started := time.Now()
	version := c.Observability.Version
	if version == "" {
		version = buildRevision()
	}
	if version == "" {
		version = "unresolved"
	}
	instance := c.Observability.InstanceID
	if instance == "" {
		instance = uuid.NewString()
	}
	queueSize := c.Observability.LogQueueSize
	if queueSize == 0 {
		queueSize = 512
	}
	writer, err := telemetry.NewWriter(os.Stdout, telemetry.Metadata{Service: "ridethewind.knowledge", Environment: c.Mode, Version: version, Instance: instance}, queueSize)
	if err != nil {
		bootstrapFailure("knowledge.observability.failed", err)
		return err
	}
	if err = writer.Install(c.Log.Level); err != nil {
		_ = writer.Close()
		return err
	}
	var observer *telemetry.Runtime
	defer func() {
		flushTimeout := c.Observability.ShutdownTimeoutMillis
		if flushTimeout == 0 {
			flushTimeout = 3000
		}
		if observer != nil {
			shutdown, cancel := context.WithTimeout(context.Background(), time.Duration(observer.Config.ShutdownTimeoutMillis)*time.Millisecond)
			if err := observer.Close(shutdown); err != nil {
				telemetry.Process(context.Background(), "knowledge.telemetry.shutdown_failed", "trace shutdown failed", "failed", started, err)
				if result == nil {
					result = err
				}
			}
			cancel()
		}
		preflight, cancelPreflight := context.WithTimeout(context.Background(), time.Duration(flushTimeout)*time.Millisecond)
		if err := writer.Flush(preflight); err != nil {
			telemetry.Process(context.Background(), "knowledge.telemetry.flush_failed", "log flush failed", "failed", started, err)
			if result == nil {
				result = err
			}
		}
		cancelPreflight()
		if result != nil {
			telemetry.Process(context.Background(), "knowledge.service.failed", "knowledge service failed", "failed", started, result, logx.Field("log_records_dropped", writer.Dropped()))
		} else {
			telemetry.Process(context.Background(), "knowledge.service.stopped", "knowledge service stopped", "succeeded", started, nil, logx.Field("log_records_dropped", writer.Dropped()))
		}
		logx.Reset()
		flush, cancel := context.WithTimeout(context.Background(), time.Duration(flushTimeout)*time.Millisecond)
		if err := writer.CloseContext(flush); err != nil && result == nil {
			result = err
		}
		cancel()
	}()
	if version == "unresolved" {
		return fmt.Errorf("knowledge service version must be a build revision or explicitly configured")
	}
	observer, err = telemetry.New(context.Background(), c.Observability, writer, telemetry.Metadata{Service: "ridethewind.knowledge", Environment: c.Mode, Version: version, Instance: instance})
	if err != nil {
		return fmt.Errorf("configure observability: %w", err)
	}
	observer.Install()
	c.RestConf.Telemetry.Disabled = true
	c.Middlewares.Trace = false
	c.Middlewares.Log = false
	c.Middlewares.Prometheus = false
	ctx, err := svc.NewServiceContext(c, observer)
	if err != nil {
		return err
	}
	defer ctx.Close()
	lifecycle, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	defer workers.Wait()
	defer cancel()
	proc.AddShutdownListener(cancel)
	if c.Delivery.Enabled {
		if c.Delivery.Endpoint == "" || c.Delivery.IntervalMillis < 100 {
			return fmt.Errorf("delivery endpoint and interval >= 100ms required")
		}
		sender := &mqs.HTTPSender{Endpoint: c.Delivery.Endpoint, Token: c.Delivery.Token, Client: &http.Client{Timeout: 10 * time.Second}}
		workers.Add(1)
		go func() {
			defer workers.Done()
			mqs.Run(lifecycle, ctx.Store, sender, time.Duration(c.Delivery.IntervalMillis)*time.Millisecond)
		}()
	}
	handler.ConfigureResponses()
	server, err := rest.NewServer(c.RestConf, rest.WithRouter(telemetry.NewRouter(observer, c.Name)))
	if err != nil {
		return err
	}
	logx.SetWriter(writer)
	handler.RegisterHandlers(server, ctx)
	metricHandler := observer.Metrics()
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/metrics", Handler: func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-Knowledge-Instance-ID", instance)
		metricHandler.ServeHTTP(w, req)
	}})
	telemetry.Process(context.Background(), "knowledge.service.starting", "knowledge service starting", "started", started, nil,
		logx.Field("listen_addr", fmt.Sprintf("%s:%d", c.Host, c.Port)),
		logx.Field("trace_export_enabled", c.Observability.Endpoint != ""),
		logx.Field("trace_sample_ratio", c.Observability.SampleRatio),
		logx.Field("log_queue_capacity", queueSize),
		logx.Field("trace_queue_capacity", observer.Config.QueueSize))
	readyContext, cancelReady := context.WithCancel(context.Background())
	readyDone := make(chan struct{})
	go func() {
		defer close(readyDone)
		waitForReady(readyContext, c.Host, c.Port, instance, started)
	}()
	result = startServer(server)
	cancelReady()
	<-readyDone
	return result
}

func waitForReady(ctx context.Context, host string, port int, instance string, started time.Time) {
	if host == "0.0.0.0" || host == "" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	endpoint := "http://" + net.JoinHostPort(host, fmt.Sprint(port)) + "/metrics"
	client := &http.Client{Timeout: 200 * time.Millisecond}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err == nil {
			res, requestErr := client.Do(req)
			if requestErr == nil {
				verified := res.StatusCode == http.StatusOK && res.Header.Get("X-Knowledge-Instance-ID") == instance
				res.Body.Close()
				if verified && ctx.Err() == nil {
					telemetry.Process(context.Background(), "knowledge.service.started", "knowledge listener ready", "succeeded", started, nil, logx.Field("listen_addr", net.JoinHostPort(host, fmt.Sprint(port))))
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func startServer(server *rest.Server) (result error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if err, ok := recovered.(error); ok {
				result = fmt.Errorf("start knowledge HTTP server: %w", err)
				return
			}
			result = fmt.Errorf("start knowledge HTTP server: %v", recovered)
		}
	}()
	server.Start()
	return nil
}
