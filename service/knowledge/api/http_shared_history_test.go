package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// productHistorySharedReady is a disposable test rendezvous. No client is
// allowed to derive RTW identity from WhaleHall's separate DC account.
type productHistoryCitationExpectation struct {
	AnswerID   string `json:"answer_id"`
	EvidenceID string `json:"evidence_id"`
	RevisionID string `json:"revision_id"`
	QuoteHash  string `json:"quote_hash"`
	State      string `json:"state"`
}

type productHistorySharedReady struct {
	Stage                  string                              `json:"stage"`
	BaseURL                string                              `json:"base_url"`
	ProductToken           string                              `json:"product_token"`
	OtherToken             string                              `json:"other_token"`
	SessionID              string                              `json:"session_id"`
	OtherSessionID         string                              `json:"other_session_id"`
	AnswerIDs              []string                            `json:"answer_ids"`
	AcceptedOrdinals       []int64                             `json:"accepted_ordinals"`
	ExpectedStatuses       []string                            `json:"expected_statuses"`
	ExpectedQuote          string                              `json:"expected_quote"`
	ExpectedCitationStates []productHistoryCitationExpectation `json:"expected_citation_states"`
}

func sharedProductHistoryReady(baseURL, token, otherToken, sessionID,
	firstAnswer, secondAnswer, firstStatus, secondStatus,
	quote, evidenceID, revisionID, quoteHash, state string) productHistorySharedReady {
	ready := productHistorySharedReady{BaseURL: baseURL, ProductToken: token,
		OtherToken: otherToken, SessionID: sessionID, OtherSessionID: "other-session",
		AnswerIDs: []string{firstAnswer, secondAnswer}, AcceptedOrdinals: []int64{1, 2},
		ExpectedStatuses: []string{firstStatus, secondStatus}, ExpectedQuote: quote}
	for _, answerID := range ready.AnswerIDs {
		ready.ExpectedCitationStates = append(ready.ExpectedCitationStates, productHistoryCitationExpectation{
			AnswerID: answerID, EvidenceID: evidenceID, RevisionID: revisionID,
			QuoteHash: quoteHash, State: state})
	}
	return ready
}

func waitSharedProductHistory(t *testing.T, stage string, ready productHistorySharedReady) {
	t.Helper()
	paths := []string{os.Getenv("KNOWLEDGE_SHARED_HISTORY_READY"), os.Getenv("KNOWLEDGE_SHARED_HISTORY_RELEASE"),
		os.Getenv("KNOWLEDGE_SHARED_HISTORY_WITHDRAWN_READY"), os.Getenv("KNOWLEDGE_SHARED_HISTORY_WITHDRAWN_RELEASE")}
	if paths[0] == "" && paths[1] == "" && paths[2] == "" && paths[3] == "" {
		return
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if !filepath.IsAbs(path) || seen[path] {
			t.Fatal("shared product history requires four distinct absolute handoff paths")
		}
		seen[path] = true
	}
	if stage != "available" && stage != "withdrawn" {
		t.Fatal("invalid shared product history stage")
	}
	index := 0
	if stage == "withdrawn" {
		index = 2
	}
	ready.Stage = stage
	body, err := json.Marshal(ready)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(filepath.Dir(paths[index]), ".knowledge-history-ready-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if _, err := file.Write(body); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(file.Name(), paths[index]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(paths[index]) })
	deadline := time.NewTimer(4 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatalf("shared product history %s release timed out", stage)
		case <-ticker.C:
			if _, err := os.Stat(paths[index+1]); err == nil {
				return
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		}
	}
}
