// Command fact-dispatch sends committed comment facts to DataCenter. It runs
// outside comment RPC availability and never rebuilds a source envelope.
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

	"sea-try-go/service/comment/rpc/internal/model"
	"sea-try-go/service/common/communityfact"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	once := flag.Bool("once", false, "dispatch at most one pending comment fact")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("service", "rtw-comment-fact-dispatch")
	slog.SetDefault(logger)
	if err := run(*once, logger); err != nil {
		logger.Error("comment fact dispatcher stopped", "event", "comment.fact_delivery.stopped",
			"outcome", "failed", "error_code", "DELIVERY_FAILED", "error_type", "process")
		os.Exit(1)
	}
}

func run(once bool, logger *slog.Logger) error {
	dsn := os.Getenv("COMMENT_DATABASE_URL")
	endpoint := os.Getenv("DC_PLATFORM_EVENT_URL")
	token := os.Getenv("DC_PLATFORM_SERVICE_TOKEN")
	if dsn == "" || endpoint == "" || len(token) < 32 {
		return errors.New("comment fact dispatcher configuration incomplete")
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
	sender, err := communityfact.NewDCEventSender(endpoint, token, &http.Client{Timeout: 8 * time.Second})
	if err != nil {
		return err
	}
	return communityfact.RunDispatcher(ctx, model.NewCommentModel(db), sender, logger, once)
}
