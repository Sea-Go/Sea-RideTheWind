// Command fact-authority serves an opt-in internal source read for target
// reactions after the corresponding DataCenter receipt is committed locally.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sea-try-go/service/common/communityfact"
	"sea-try-go/service/like/rpc/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	started := time.Now()
	meta := communityfact.ProcessMetadata{Service: "rtw-like-fact-authority", Environment: os.Getenv("RTW_ENVIRONMENT"),
		Version: os.Getenv("RTW_SERVICE_VERSION"), InstanceID: os.Getenv("RTW_INSTANCE_ID"), Component: "community"}
	logger := communityfact.NewProcessLogger(os.Stderr, meta)
	tracing := communityfact.InstallLocalTracing()
	defer tracing.Shutdown(context.Background())
	err := meta.Validate()
	if err == nil {
		err = run(logger)
	}
	if err != nil {
		logger.Error("like fact authority stopped", "event", "like.fact_authority.stopped",
			"outcome", "failed", "duration_ms", float64(time.Since(started).Microseconds())/1000,
			"error_code", "AUTHORITY_UNAVAILABLE", "error_type", "process")
		os.Exit(1)
	}
	logger.Info("like fact authority stopped", "event", "like.fact_authority.stopped",
		"outcome", "succeeded", "duration_ms", float64(time.Since(started).Microseconds())/1000)
}

func run(logger *slog.Logger) error {
	dsn := os.Getenv("LIKE_DATABASE_URL")
	listen := os.Getenv("LIKE_FACT_AUTHORITY_LISTEN")
	token := os.Getenv("LIKE_FACT_AUTHORITY_TOKEN")
	if dsn == "" || listen == "" || len(token) < 32 {
		return errors.New("like fact authority configuration incomplete")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := sqlDB.PingContext(ctx); err != nil {
		return err
	}
	handler, err := communityfact.NewAuthorityHandler("rtw.like-mq", token, model.NewLikeFactModel(db).AuthoritativeFact, logger)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second,
		ErrorLog: slog.NewLogLogger(logger.With("event", "like.fact_authority.http_error").Handler(), slog.LevelError)}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	logger.InfoContext(ctx, "like fact authority started", "event", "like.fact_authority.started",
		"address", listener.Addr().String())
	select {
	case err := <-finished:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
