package telemetry

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A human judgment commits domain state, while reading its original Event is
// only a source lookup. Their bounded operation names must reach JSON/metrics
// unchanged instead of silently folding into knowledge.unknown.
func TestSearchJudgmentWriteAndEventReadHaveDistinctObservation(t *testing.T) {
	var output bytes.Buffer
	writer, err := NewWriter(&output, testMetadata, 32)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Install("info"); err != nil {
		t.Fatal(err)
	}
	runtime, err := New(context.Background(), Config{SampleRatio: 1}, writer, testMetadata)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"knowledge.search.judgment.record",
		"knowledge.search.judgment.event.read"} {
		_, finish := runtime.Begin(context.Background(), operation, "judgment-source-fixture", nil)
		finish(nil, ErrorInfo{}, nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := writer.CloseContext(ctx); err != nil {
		t.Fatal(err)
	}
	jsonLines := output.String()
	if !strings.Contains(jsonLines, `"event":"knowledge.search.judgment.record.succeeded"`) ||
		!strings.Contains(jsonLines, `"event":"knowledge.search.judgment.event.read.succeeded"`) ||
		strings.Contains(jsonLines, `"knowledge.unknown"`) {
		t.Fatalf("human judgment was observed under the wrong source operation: %s", jsonLines)
	}
	metrics := httptest.NewRecorder()
	runtime.Metrics().ServeHTTP(metrics, httptest.NewRequest("GET", "/metrics", nil))
	body := metrics.Body.String()
	if !strings.Contains(body, `sea_knowledge_commits_total{operation="knowledge.search.judgment.record"} 1`) ||
		strings.Contains(body, `sea_knowledge_commits_total{operation="knowledge.search.judgment.event.read"}`) {
		t.Fatalf("Event GET was counted as a human domain commitment: %s", body)
	}
}
