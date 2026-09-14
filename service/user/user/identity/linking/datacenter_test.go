package linking

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestDCVerifierCurrentSessionContract(t *testing.T) {
	id := uuid.NewString()
	var redirected atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(true)
	}))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/auth/me" {
			http.NotFound(w, r)
			return
		}
		switch r.Header.Get("Authorization") {
		case "Bearer wh_access_valid":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"` + id + `","email":"untrusted@example.test"}`))
		case "Bearer wh_access_revoked":
			w.WriteHeader(http.StatusUnauthorized)
		case "Bearer wh_access_down":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "Bearer wh_access_wrong_id":
			_, _ = w.Write([]byte(`{"id":"not-a-uuid"}`))
		case "Bearer wh_access_large":
			_, _ = w.Write([]byte(`{"id":"` + id + `","extra":"` + strings.Repeat("x", 4096) + `"}`))
		case "Bearer wh_access_redirect":
			http.Redirect(w, r, destination.URL, http.StatusFound)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()
	verifier, err := NewDCVerifier(server.URL+"/v1/auth/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := verifier.CurrentUserID(context.Background(), "wh_access_valid"); err != nil || got != id {
		t.Fatalf("current DC session UUID: %s %v", got, err)
	}
	for _, test := range []struct {
		token string
		want  error
	}{
		{"wh_access_revoked", ErrUnauthenticated},
		{"wh_access_down", ErrUnavailable},
		{"wh_access_wrong_id", ErrUnavailable},
		{"wh_access_large", ErrUnavailable},
		{"wh_access_redirect", ErrUnavailable},
		{"jwt-looking-value", ErrUnauthenticated},
	} {
		if _, err := verifier.CurrentUserID(context.Background(), test.token); !errors.Is(err, test.want) {
			t.Fatalf("%s: got %v, want %v", test.token, err, test.want)
		}
	}
	if redirected.Load() {
		t.Fatal("DC bearer was forwarded to a redirected endpoint")
	}
	if _, err := NewDCVerifier("http://datacenter.example/v1/auth/me", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-loopback HTTP accepted: %v", err)
	}
	if _, err := NewDCVerifier(server.URL+"/v1/auth/me?redirect=1", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("query-bearing auth endpoint accepted: %v", err)
	}
}
