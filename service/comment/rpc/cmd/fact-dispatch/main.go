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

func main() { os.Exit(execute()) }

func execute() int {
	once := flag.Bool("once", false, "dispatch at most one pending comment fact")
	flag.Parse()
	started := time.Now()
	meta := communityfact.ProcessMetadata{Service: "rtw-comment-fact-dispatch", Environment: os.Getenv("RTW_ENVIRONMENT"),
		Version: os.Getenv("RTW_SERVICE_VERSION"), InstanceID: os.Getenv("RTW_INSTANCE_ID"), Component: "community"}
	logger := communityfact.NewProcessLogger(os.Stderr, meta)
	tracing := communityfact.InstallLocalTracing()
	defer tracing.Shutdown(context.Background())
	err := meta.Validate()
	if err == nil {
		err = communityfact.InstallFrameworkLogger(logger)
	}
	if err == nil {
		err = run(*once, logger)
	}
	if err != nil {
		logger.Error("comment fact dispatcher stopped", "event", "comment.fact_delivery.stopped",
			"outcome", "failed", "duration_ms", float64(time.Since(started).Microseconds())/1000,
			"error_code", "DELIVERY_FAILED", "error_type", "process")
		return 1
	}
	logger.Info("comment fact dispatcher stopped", "event", "comment.fact_delivery.stopped",
		"outcome", "succeeded", "duration_ms", float64(time.Since(started).Microseconds())/1000)
	return 0
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
