// fact-authority is an opt-in private RTW source read for downstream binders.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sea-try-go/service/favorite/rpc/internal/authority"
	"sea-try-go/service/favorite/rpc/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("service", "rtw-favorite-fact-authority")
	slog.SetDefault(logger)
	if err := run(); err != nil {
		logger.Error("favorite authority stopped", "event", "favorite.authority.stopped",
			"outcome", "failed", "error_code", "AUTHORITY_UNAVAILABLE")
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("FAVORITE_DATABASE_URL")
	listen := os.Getenv("FAVORITE_AUTHORITY_LISTEN")
	token := os.Getenv("FAVORITE_AUTHORITY_TOKEN")
	if dsn == "" || listen == "" || len(token) < 32 {
		return errors.New("favorite authority configuration incomplete")
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
	handler, err := authority.NewHandler(model.NewFavoriteModel(db), token)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := sqlDB.PingContext(ctx); err != nil {
		return err
	}
	server := &http.Server{Addr: listen, Handler: handler,
		ReadHeaderTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second}
	finished := make(chan error, 1)
	go func() { finished <- server.ListenAndServe() }()
	select {
	case err := <-finished:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}
