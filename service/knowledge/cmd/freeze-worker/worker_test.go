package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sea-try-go/service/knowledge/internal/structure"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func shaHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

const (
	docASource = "# Book A\n\nEvidence paragraph one.\n\n## Chapter\n\nEvidence paragraph two.\n"
	docBSource = "# 页面 B\n\n第一段：整篇不切割。\n\n## 背景\n\n背景段落，含中文与 \"引号\"。\n"
	docCSource = "# Summary C\n\nSummary body.\n"
	sampleLine = `{"module_id":"mod-1","release_id":"rel-1","published_at":"2026-10-06T12:00:00Z","docs":[` +
		`{"revision_id":"rev-a","doc_key":"source:11111111-2222-3333-4444-555555555555@v1","source_b64":"` + "%s" + `"},` +
		`{"revision_id":"rev-b","doc_key":"page:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee@v2","source_b64":"` + "%s" + `"},` +
		`{"revision_id":"rev-c","doc_key":"summary:00000000-1111-2222-3333-444444444444@v3","source_b64":"` + "%s" + `"}]}`
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "requests.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return path
}

func runWorker(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func eventIDFrom(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "released ") {
			for _, field := range strings.Fields(line) {
				if strings.HasPrefix(field, "event_id=") {
					return strings.TrimPrefix(field, "event_id=")
				}
			}
		}
	}
	t.Fatalf("no event_id in output:\n%s", out)
	return ""
}

func TestWorkerReleaseDeterministicAndIdempotent(t *testing.T) {
	line := strings.Replace(sampleLine, "%s", b64(docASource), 1)
	line = strings.Replace(line, "%s", b64(docBSource), 1)
	line = strings.Replace(line, "%s", b64(docCSource), 1)
	path := writeTemp(t, line+"\n")

	code, out1, stderr := runWorker(t, "--input", path, "--accept")
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if got := strings.Count(out1, "frozen "); got != 3 {
		t.Fatalf("expected 3 frozen lines, got %d:\n%s", got, out1)
	}
	if !strings.Contains(out1, "locator ") || !strings.Contains(out1, "accepted ") {
		t.Fatalf("accept demo missing from output:\n%s", out1)
	}
	id1 := eventIDFrom(t, out1)

	// A second run over the same input replays identically (same event id).
	code, out2, stderr := runWorker(t, "--input", path)
	if code != 0 {
		t.Fatalf("second run exit %d, stderr:\n%s", code, stderr)
	}
	if id2 := eventIDFrom(t, out2); id2 != id1 {
		t.Fatalf("same input produced different event ids: %s vs %s", id2, id1)
	}
}

func TestWorkerAcceptPrintsRealQuote(t *testing.T) {
	line := strings.Replace(sampleLine, "%s", b64(docASource), 1)
	line = strings.Replace(line, "%s", b64(docBSource), 1)
	line = strings.Replace(line, "%s", b64(docCSource), 1)
	path := writeTemp(t, line+"\n")

	code, out, stderr := runWorker(t, "--input", path, "--accept")
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	// The demo quotes the first paragraph of the first doc (Book A), which
	// sits under the "Book A" heading at para 0.
	if !strings.Contains(out, `locator para_index=0 section_path="Book A" quote="Evidence paragraph one."`) {
		t.Fatalf("unexpected locator line:\n%s", out)
	}
	if !strings.Contains(out, "tree_sha=") || !strings.Contains(out, "citations=1") {
		t.Fatalf("unexpected receipt line:\n%s", out)
	}
}

func TestWorkerRejectsStaleSHA(t *testing.T) {
	req := map[string]any{
		"module_id": "mod-1", "release_id": "rel-1",
		"published_at": "2026-10-06T12:00:00Z",
		"docs": []map[string]any{{
			"revision_id":    "rev-a",
			"doc_key":        "source:11111111-2222-3333-4444-555555555555@v1",
			"content_sha256": strings.Repeat("0", 64),
			"source_b64":     b64(docASource),
		}},
	}
	data, _ := json.Marshal(req)
	path := writeTemp(t, string(data)+"\n")
	code, _, stderr := runWorker(t, "--input", path)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "does not match source bytes") {
		t.Fatalf("unexpected stderr:\n%s", stderr)
	}
}

func TestWorkerComputesOmittedSHA(t *testing.T) {
	req := map[string]any{
		"module_id": "mod-1", "release_id": "rel-1",
		"published_at": "2026-10-06T12:00:00Z",
		"docs": []map[string]any{{
			"revision_id": "rev-a",
			"doc_key":     "source:11111111-2222-3333-4444-555555555555@v1",
			"source_b64":  b64(docASource),
		}},
	}
	data, _ := json.Marshal(req)
	path := writeTemp(t, string(data)+"\n")
	code, out, stderr := runWorker(t, "--input", path)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if !strings.Contains(out, "content_sha256="+shaHex(docASource)) {
		t.Fatalf("computed sha missing from output:\n%s", out)
	}
}

func TestWorkerUsage(t *testing.T) {
	if code, _, _ := runWorker(t); code != 2 {
		t.Fatalf("expected exit 2 without --input, got %d", code)
	}
	if code, _, _ := runWorker(t, "--input"); code != 2 {
		t.Fatalf("expected exit 2 for dangling --input, got %d", code)
	}
	code, _, stderr := runWorker(t, "--input", filepath.Join(t.TempDir(), "missing.jsonl"))
	if code != 1 || !strings.Contains(stderr, "open") {
		t.Fatalf("expected exit 1 for missing file, got %d:\n%s", code, stderr)
	}
}

func TestReadRequestsRejectsUnknownField(t *testing.T) {
	_, err := readRequests(strings.NewReader(`{"module_id":"m","unexpected":1}` + "\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field rejection, got %v", err)
	}
}

func TestSectionPathOfMatchesAnchorSemantics(t *testing.T) {
	source := docBSource
	tree := mustDerive(t, "rev-b", source)
	for _, idx := range tree.Paragraphs() {
		node := tree.Nodes[idx]
		path := sectionPathOf(tree, idx)
		want := "页面 B / 背景"
		if node.ParaIndex == 0 {
			want = "页面 B"
		}
		if got := strings.Join(path, " / "); got != want {
			t.Fatalf("para %d path %q != %q", node.ParaIndex, got, want)
		}
	}
}

func mustDerive(t *testing.T, revID, source string) *structure.Tree {
	t.Helper()
	tree, err := structure.Derive(revID, []byte(source))
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	return tree
}
