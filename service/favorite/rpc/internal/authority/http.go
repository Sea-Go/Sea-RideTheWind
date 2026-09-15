// Package authority exposes frozen favorite fact evidence to trusted services.
package authority

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"sea-try-go/service/favorite/rpc/internal/model"
)

type Reader interface {
	AuthoritativeFavoriteFact(context.Context, string, string) (model.FavoriteAuthorityFact, error)
}

type ReaderV2 interface {
	AuthoritativeFavoriteFactV2(context.Context, string, string) (model.FavoriteAuthorityFactV2, error)
}

// NewHandler is private-service only. Configuration/launch rejects missing or
// short tokens; every response is uncached and never includes article content.
func NewHandler(reader Reader, token string) (http.Handler, error) {
	if reader == nil || len(token) < 32 {
		return nil, errors.New("favorite authority is not configured")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/favorite/facts/{producer}/{event_id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		value := r.Header.Values("Authorization")
		if len(value) != 1 || !strings.HasPrefix(value[0], "Bearer ") ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(value[0], "Bearer ")), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		fact, err := reader.AuthoritativeFavoriteFact(ctx, r.PathValue("producer"), r.PathValue("event_id"))
		if errors.Is(err, model.ErrFavoriteFactUnavailable) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			slog.ErrorContext(ctx, "favorite authority lookup failed", "event", "favorite.authority.lookup_failed",
				"error_code", "SOURCE_READ_FAILED")
			http.Error(w, "source unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fact); err != nil {
			slog.WarnContext(ctx, "favorite authority response interrupted", "event", "favorite.authority.response_interrupted",
				"error_code", "WRITE_FAILED")
		}
	})
	mux.HandleFunc("GET /internal/v2/favorite/facts/{producer}/{event_id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		value := r.Header.Values("Authorization")
		if len(value) != 1 || !strings.HasPrefix(value[0], "Bearer ") ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(value[0], "Bearer ")), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		v2, ok := reader.(ReaderV2)
		if !ok {
			http.NotFound(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		fact, err := v2.AuthoritativeFavoriteFactV2(ctx, r.PathValue("producer"), r.PathValue("event_id"))
		if errors.Is(err, model.ErrFavoriteFactUnavailable) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			slog.ErrorContext(ctx, "favorite v2 authority lookup failed", "event", "favorite.authority.v2.lookup_failed",
				"error_code", "SOURCE_READ_FAILED")
			http.Error(w, "source unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fact); err != nil {
			slog.WarnContext(ctx, "favorite v2 authority response interrupted", "event", "favorite.authority.v2.response_interrupted",
				"error_code", "WRITE_FAILED")
		}
	})
	return mux, nil
}
