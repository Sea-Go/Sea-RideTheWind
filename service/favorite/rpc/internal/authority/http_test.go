package authority

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"sea-try-go/service/favorite/rpc/internal/model"
)

type stubReader struct{ calls int }

func (r *stubReader) AuthoritativeFavoriteFact(context.Context, string, string) (model.FavoriteAuthorityFact, error) {
	r.calls++
	return model.FavoriteAuthorityFact{}, model.ErrFavoriteFactUnavailable
}

func TestFavoriteAuthorityHTTPAuthenticationAndNoCallerScope(t *testing.T) {
	reader := &stubReader{}
	if _, err := NewHandler(reader, "short"); err == nil {
		t.Fatal("short service token enabled authority endpoint")
	}
	if _, err := NewHandler(nil, "test-only-rtw-favorite-source-token-123456"); err == nil {
		t.Fatal("missing source reader enabled authority endpoint")
	}
	const token = "test-only-rtw-favorite-source-token-123456"
	handler, err := NewHandler(reader, token)
	if err != nil {
		t.Fatal(err)
	}
	path := "/internal/v1/favorite/facts/rtw.community.favorite/favorite.1.v1"
	for _, tc := range []struct {
		name       string
		headers    []string
		path       string
		wantStatus int
		wantCalls  int
	}{
		{"missing", nil, path, 401, 0},
		{"wrong", []string{"Bearer wrong"}, path, 401, 0},
		{"duplicate", []string{"Bearer " + token, "Bearer " + token}, path, 401, 0},
		{"scope-query", []string{"Bearer " + token}, path + "?subject_id=other", 404, 0},
		{"source-miss", []string{"Bearer " + token}, path, 404, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			for _, header := range tc.headers {
				request.Header.Add("Authorization", header)
			}
			before := reader.calls
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.wantStatus || reader.calls-before != tc.wantCalls ||
				response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d source_calls=%d cache=%q", response.Code,
					reader.calls-before, response.Header().Get("Cache-Control"))
			}
		})
	}
}
