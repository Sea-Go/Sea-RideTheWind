// fact-dispatch delivers committed RTW favorite facts to DataCenter eventing.
// It is a separate process so favorite RPC availability does not depend on DC.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sea-try-go/service/favorite/rpc/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	once := flag.Bool("once", false, "dispatch at most one pending favorite event")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("service", "rtw-favorite-fact-dispatch")
	slog.SetDefault(logger)
	if err := run(*once, logger); err != nil {
		logger.Error("favorite delivery stopped", "event", "favorite.delivery.stopped", "outcome", "failed",
			"error_code", "DELIVERY_FAILED")
		os.Exit(1)
	}
}

func run(once bool, logger *slog.Logger) error {
	dsn := os.Getenv("FAVORITE_DATABASE_URL")
	endpoint := os.Getenv("DC_PLATFORM_EVENT_URL")
	token := os.Getenv("DC_PLATFORM_SERVICE_TOKEN")
	if dsn == "" || endpoint == "" || token == "" {
		return errors.New("favorite delivery configuration incomplete")
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
	store := model.NewFavoriteModel(db)
	sender := model.NewFavoriteDCEventSender(endpoint, token, &http.Client{Timeout: 8 * time.Second})
	if once {
		attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		sent, err := store.DispatchFavoriteFactOnce(attempt, sender)
		if err != nil {
			return err
		}
		logger.Info("favorite delivery inspected", "event", "favorite.delivery.once", "outcome", "succeeded", "sent", sent)
		return nil
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		for i := 0; i < 16; i++ {
			attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
			sent, dispatchErr := store.DispatchFavoriteFactOnce(attempt, sender)
			cancel()
			if dispatchErr != nil {
				logger.Warn("favorite delivery retry scheduled", "event", "favorite.delivery.retry",
					"outcome", "retryable", "error_code", "DELIVERY_ATTEMPT_FAILED")
				break
			}
			if !sent {
				break
			}
			logger.Info("favorite fact delivered", "event", "favorite.delivery.accepted", "outcome", "succeeded")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
