package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"time"

	"sea-try-go/service/favorite/rpc/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Read-only migration gate for operators. It neither auto-approves old rows
// nor changes frozen Outbox data; a blocked report exits 2.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("service", "rtw-favorite-legacy-preflight")
	dsn := os.Getenv("FAVORITE_DATABASE_URL")
	if dsn == "" {
		logger.Error("favorite migration preflight not configured", "event", "favorite.legacy_preflight.rejected",
			"error_code", "DATABASE_URL_MISSING")
		os.Exit(2)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		logger.Error("favorite migration preflight cannot open database", "event", "favorite.legacy_preflight.failed",
			"error_code", "DATABASE_OPEN_FAILED")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := model.NewFavoriteModel(db).FavoriteLegacyPreflight(ctx)
	if err != nil {
		logger.Error("favorite migration preflight read failed", "event", "favorite.legacy_preflight.failed",
			"error_code", "SOURCE_GRAPH_READ_FAILED")
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		logger.Error("favorite migration preflight report failed", "event", "favorite.legacy_preflight.failed",
			"error_code", "REPORT_WRITE_FAILED")
		os.Exit(1)
	}
	if !report.Clear() {
		logger.Warn("favorite migration preflight blocked", "event", "favorite.legacy_preflight.blocked",
			"error_code", "LEGACY_SOURCE_UNDECIDED", "missing_assert", report.MissingAssert,
			"approved_legacy", report.ApprovedLegacy, "blocked", report.Blocked,
			"orphan_retracts", report.OrphanRetracts)
		os.Exit(2)
	}
	logger.Info("favorite migration preflight clear", "event", "favorite.legacy_preflight.clear",
		"missing_assert", report.MissingAssert, "approved_legacy", report.ApprovedLegacy)
}
