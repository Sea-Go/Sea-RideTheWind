// Command fact-authority serves an opt-in internal read from comment-owned
// source state. It returns only facts carrying a matching DataCenter receipt.
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

	"sea-try-go/service/comment/rpc/internal/model"
	"sea-try-go/service/common/communityfact"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("service", "rtw-comment-fact-authority")
	slog.SetDefault(logger)
	if err := run(logger); err != nil {
		logger.Error("comment fact authority stopped", "event", "comment.fact_authority.stopped",
			"outcome", "failed", "error_code", "AUTHORITY_UNAVAILABLE", "error_type", "process")
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	dsn := os.Getenv("COMMENT_DATABASE_URL")
	listen := os.Getenv("COMMENT_FACT_AUTHORITY_LISTEN")
	token := os.Getenv("COMMENT_FACT_AUTHORITY_TOKEN")
	if dsn == "" || listen == "" || len(token) < 32 {
		return errors.New("comment fact authority configuration incomplete")
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
	handler, err := communityfact.NewAuthorityHandler("rtw.comment-rpc", token, model.NewCommentModel(db).AuthoritativeFact)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second,
		ErrorLog: slog.NewLogLogger(logger.With("event", "comment.fact_authority.http_error").Handler(), slog.LevelError)}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	logger.InfoContext(ctx, "comment fact authority started", "event", "comment.fact_authority.started",
		"outcome", "succeeded", "address", listener.Addr().String())
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
