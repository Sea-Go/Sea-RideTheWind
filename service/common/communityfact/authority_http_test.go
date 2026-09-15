package communityfact

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testAuthorityToken = "community-authority-test-token-at-least-32-bytes"

func testProcessLogger(output *bytes.Buffer) *slog.Logger {
	return NewProcessLogger(output, ProcessMetadata{Service: "rtw-community-test", Environment: "test",
		Version: strings.Repeat("a", 40), InstanceID: "test-1", Component: "community"})
}

func TestAuthorityHandlerUsesInjectedLogger(t *testing.T) {
	var output bytes.Buffer
	handler, err := NewAuthorityHandler("rtw.comment-rpc", testAuthorityToken,
		func(context.Context, string, string) (AuthorityFact, error) {
			return AuthorityFact{}, errors.New("storage unavailable")
		},
		testProcessLogger(&output))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet,
		"/internal/v1/community/facts?producer=rtw.comment-rpc&event_id=known", nil)
	request.Header.Set("Authorization", "Bearer "+testAuthorityToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(output.String(), `"event":"community.fact_authority.lookup_failed"`) ||
		!strings.Contains(output.String(), `"event":"community.fact_authority.request_finished"`) ||
		!strings.Contains(output.String(), `"duration_ms":`) {
		t.Fatalf("injected authority log missing: status=%d log=%s", response.Code, output.String())
	}
	for _, key := range []string{"timestamp", "level", "service", "environment", "service_version", "instance_id", "component", "log_source", "event", "message"} {
		if !strings.Contains(output.String(), `"`+key+`":`) {
			t.Fatalf("OBS-r3 key %s missing: %s", key, output.String())
		}
	}
}

type failingResponseWriter struct{ header http.Header }

func (w *failingResponseWriter) Header() http.Header { return w.header }
func (*failingResponseWriter) WriteHeader(int)       {}
func (*failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("connection closed")
}

func TestAuthorityHandlerLogsInterruptedResponseThroughInjection(t *testing.T) {
	var output bytes.Buffer
	handler, err := NewAuthorityHandler("rtw.comment-rpc", testAuthorityToken,
		func(context.Context, string, string) (AuthorityFact, error) {
			return AuthorityFact{Event: Event{EventID: "known"}}, nil
		}, testProcessLogger(&output))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet,
		"/internal/v1/community/facts?producer=rtw.comment-rpc&event_id=known", nil)
	request.Header.Set("Authorization", "Bearer "+testAuthorityToken)
	handler.ServeHTTP(&failingResponseWriter{header: make(http.Header)}, request)
	if !strings.Contains(output.String(), `"event":"community.fact_authority.response_interrupted"`) {
		t.Fatalf("interrupted response bypassed injected logger: %s", output.String())
	}
}
