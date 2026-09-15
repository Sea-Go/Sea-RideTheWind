package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/types"
)

const webProductSearchHandoffSchema = "sea.web.product-search-handoff.v1"

type webProductSearchReady struct {
	SchemaVersion     string `json:"schema_version"`
	Stage             string `json:"stage"`
	RTWBaseURL        string `json:"rtw_base_url"`
	UserCenterBaseURL string `json:"user_center_base_url"`
	LoginUsername     string `json:"login_username"`
	OtherUsername     string `json:"other_username"`
	LoginPassword     string `json:"login_password"`
	ModuleID          string `json:"module_id"`
	SessionID         string `json:"session_id"`
	Query             string `json:"query"`
	Depth             string `json:"depth"`
	Intelligence      string `json:"intelligence"`
	IdempotencyKey    string `json:"idempotency_key"`
}

type webProductSearchResult struct {
	SchemaVersion    string                    `json:"schema_version"`
	BFFBaseURL       string                    `json:"bff_base_url"`
	LoginHTTPStatus  int                       `json:"login_http_status"`
	PageHTTPStatus   int                       `json:"page_http_status"`
	SearchHTTPStatus int                       `json:"search_http_status"`
	ProductSearch    types.ProductSearchResult `json:"product_search"`
}

// waitWebProductSearch replaces the parent's one cited POST only when an
// external Web acceptance owns the two private rendezvous files. RTW still
// validates the returned identity against its own PostgreSQL immediately
// afterwards; the handoff file is synchronization, not answer authority.
func waitWebProductSearch(t *testing.T, users *realUserServices, rtwBaseURL, moduleID,
	sessionID, query, idempotencyKey string) (types.ProductSearchResult, bool) {
	t.Helper()
	readyPath := os.Getenv("KNOWLEDGE_WEB_SEARCH_READY")
	resultPath := os.Getenv("KNOWLEDGE_WEB_SEARCH_RESULT")
	if readyPath == "" && resultPath == "" {
		return types.ProductSearchResult{}, false
	}
	if readyPath == "" || resultPath == "" || !filepath.IsAbs(readyPath) ||
		!filepath.IsAbs(resultPath) || readyPath == resultPath || users == nil {
		t.Fatal("Web product search requires real User Center and two distinct absolute handoff paths")
	}
	for _, path := range []string{readyPath, resultPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Web product search handoff path must be absent: %s", path)
		}
		t.Cleanup(func() { _ = os.Remove(path) })
	}
	ready := webProductSearchReady{
		SchemaVersion:     webProductSearchHandoffSchema,
		Stage:             "search",
		RTWBaseURL:        rtwBaseURL,
		UserCenterBaseURL: users.apiURL,
		LoginUsername:     "knowledge-history-owner",
		OtherUsername:     "knowledge-history-other",
		LoginPassword:     "test-only-password-123",
		ModuleID:          moduleID,
		SessionID:         sessionID,
		Query:             query,
		Depth:             "fast",
		Intelligence:      "low",
		IdempotencyKey:    idempotencyKey,
	}
	body, err := json.Marshal(ready)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(filepath.Dir(readyPath), ".web-search-ready-*")
	if err != nil {
		t.Fatal(err)
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(body)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Link(tempPath, readyPath)
	}
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.NewTimer(12 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("Web product search result timed out")
		case <-ticker.C:
			info, statErr := os.Stat(resultPath)
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1<<20 {
				t.Fatalf("invalid Web product search result file: %v", statErr)
			}
			raw, readErr := os.ReadFile(resultPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			var result webProductSearchResult
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err = decoder.Decode(&result); err != nil {
				t.Fatal(err)
			}
			var trailing any
			if decoder.Decode(&trailing) != io.EOF || result.SchemaVersion != webProductSearchHandoffSchema ||
				result.BFFBaseURL == "" || result.LoginHTTPStatus != 200 || result.PageHTTPStatus != 200 ||
				result.SearchHTTPStatus != 200 {
				t.Fatalf("Web product search handoff result is incomplete: %+v", result)
			}
			return result.ProductSearch, true
		}
	}
}
