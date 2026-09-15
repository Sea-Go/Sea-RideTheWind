package communityfact

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

type AuthorityLookup func(context.Context, string, string) (AuthorityFact, error)

// NewAuthorityHandler exposes one domain-owned source lookup on an internal
// service endpoint. Producer and event ID are query parameters because RTW
// event IDs contain slashes and must remain byte-for-byte stable.
func NewAuthorityHandler(producer, token string, lookup AuthorityLookup) (http.Handler, error) {
	if producer == "" || len(token) < 32 || lookup == nil {
		return nil, errors.New("community fact authority is not configured")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/community/facts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		authorization := r.Header.Values("Authorization")
		if len(authorization) != 1 || !strings.HasPrefix(authorization[0], "Bearer ") ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(authorization[0], "Bearer ")), []byte(token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		producers, eventIDs := r.URL.Query()["producer"], r.URL.Query()["event_id"]
		if len(producers) != 1 || len(eventIDs) != 1 || producers[0] != producer || eventIDs[0] == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fact, err := lookup(r.Context(), producers[0], eventIDs[0])
		if err != nil {
			if errors.Is(err, ErrAuthorityUnavailable) {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			slog.ErrorContext(r.Context(), "community fact authority lookup failed",
				"event", "community.fact_authority.lookup_failed", "producer", producer,
				"event_id", eventIDs[0], "outcome", "failed", "error_code", "SOURCE_READ_FAILED",
				"error_type", "storage")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fact); err != nil {
			slog.WarnContext(r.Context(), "community fact authority response interrupted",
				"event", "community.fact_authority.response_interrupted", "producer", producer,
				"event_id", eventIDs[0], "outcome", "failed", "error_code", "RESPONSE_WRITE_FAILED")
		}
	})
	return mux, nil
}
