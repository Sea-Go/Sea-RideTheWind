package model_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/testenv"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

func TestToolParentFixedPublicationAndCumulativeReservations(t *testing.T) {
	s := testenv.Store(t)
	f := makeCitationFixture(t, s)
	subject := types.AcceptedSubjectRef{AuthorityId: "rtw.identity", TenantId: "platform", SubjectId: "9123"}
	const session = "tool-parent-session"
	parent, err := s.ReserveToolParent(ctx, subject, session, "tool-parent-key-1", f.module.Id)
	parent = must(t, parent, err)
	if parent.OperationID == "" || parent.BudgetRef == "" || parent.SnapshotRef == "" ||
		parent.Snapshot.ReleaseId != f.release.ReleaseId || parent.SearchRemaining != 4 ||
		parent.ReadRemaining != 24 || parent.QuoteRemaining != 32768 {
		t.Fatalf("parent did not pin publication and server budget: %+v", parent)
	}
	var parallel [4]model.ToolParent
	var errs [4]error
	var wg sync.WaitGroup
	for i := range parallel {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			parallel[i], errs[i] = s.ReserveToolParent(ctx, subject, session, "tool-parent-key-1", f.module.Id)
		}(i)
	}
	wg.Wait()
	for i := range parallel {
		if errs[i] != nil || parallel[i].OperationID != parent.OperationID ||
			!reflect.DeepEqual(parallel[i].Snapshot, parent.Snapshot) {
			t.Fatalf("parent replay selected another scope: %+v %v", parallel[i], errs[i])
		}
	}
	other := subject
	other.SubjectId = "9124"
	_, err = s.GetToolParent(ctx, other, session, parent.OperationID)
	expectError(t, err, model.ErrNotFound)
	_, err = s.GetToolParent(ctx, subject, "another-session", parent.OperationID)
	expectError(t, err, model.ErrNotFound)
	_, err = s.ReserveToolParent(ctx, subject, session, "tool-parent-key-1", "another-module")
	expectError(t, err, model.ErrConflict)

	input := model.ToolSearchInput{Query: "What is the evidence?", Depth: "fast", Intelligence: "low",
		ReadCalls: 8, QuoteRunes: 8192}
	var children [4]model.ToolSearch
	for i := range children {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			children[i], errs[i] = s.ReserveToolSearch(ctx, parent, "tool-search-key-1", input)
		}(i)
	}
	wg.Wait()
	for i := range children {
		if errs[i] != nil || children[i].SearchID != children[0].SearchID ||
			children[i].RequestHash != children[0].RequestHash {
			t.Fatalf("child idempotency lost stable RTW search ID: %+v %v", children[i], errs[i])
		}
	}
	current, err := s.GetToolParent(ctx, subject, session, parent.OperationID)
	current = must(t, current, err)
	if current.SearchRemaining != 3 || current.ReadRemaining != 16 || current.QuoteRemaining != 24576 {
		t.Fatalf("parallel same-key child overspent parent budget: %+v", current)
	}
	changed := input
	changed.Query = "a different query"
	_, err = s.ReserveToolSearch(ctx, parent, "tool-search-key-1", changed)
	expectError(t, err, model.ErrConflict)
	_, err = s.ReserveToolSearch(ctx, parent, "tool-search-key-2", model.ToolSearchInput{
		Query: "unsupported continuation", Depth: "detailed", Intelligence: "high", ContinueID: children[0].SearchID,
		ReadCalls: 8, QuoteRunes: 8192})
	expectError(t, err, model.ErrInvalid)

	claimed, yes, err := s.ClaimToolSearch(ctx, children[0])
	claimed = must(t, claimed, err)
	if !yes || claimed.SearchID != children[0].SearchID || claimed.Attempt != 1 {
		t.Fatal(claimed, yes)
	}
	_, yes, err = s.ClaimToolSearch(ctx, children[0])
	if err != nil || yes {
		t.Fatalf("active child lease was bypassed: %v claimed=%v", err, yes)
	}
	if err = s.FailToolSearch(ctx, claimed, "BTW_HTTP_502"); err != nil {
		t.Fatal(err)
	}
	retry, yes, err := s.ClaimToolSearch(ctx, children[0])
	retry = must(t, retry, err)
	if !yes || retry.SearchID != claimed.SearchID || retry.Attempt != 2 {
		t.Fatal(retry, yes)
	}
	result, _ := json.Marshal(types.ToolSearchResult{SearchId: retry.SearchID, Status: "empty",
		StopReason: "no_evidence", SnapshotRef: parent.SnapshotRef,
		RequestedIntelligence: "low", EffectiveIntelligence: "low",
		Evidence: []types.ToolEvidence{}, Gaps: []string{}, Conflicts: []string{},
		Usage: types.ToolUsage{ReadCalls: 2, QuoteRunes: 10}})
	if err = s.CompleteToolSearch(ctx, parent, retry, result, 2, 10, false); err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteToolSearch(ctx, parent, retry, result, 2, 10, false); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("second completion refunded twice: %v", err)
	}
	current, err = s.GetToolParent(ctx, subject, session, parent.OperationID)
	current = must(t, current, err)
	if current.SearchRemaining != 3 || current.ReadRemaining != 22 || current.QuoteRemaining != 32758 {
		t.Fatalf("verified usage refund wrong: %+v", current)
	}
	// Independent keys serialize on the same parent row. Only two of three
	// 8-read reservations fit the 22 remaining reads.
	var siblings [3]model.ToolSearch
	var siblingErrs [3]error
	for i := range siblings {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			siblings[i], siblingErrs[i] = s.ReserveToolSearch(ctx, parent,
				fmt.Sprintf("tool-sibling-key-%d", i), input)
		}(i)
	}
	wg.Wait()
	passed, exhausted := 0, 0
	for i := range siblings {
		if siblingErrs[i] == nil {
			passed++
		} else if errors.Is(siblingErrs[i], model.ErrConflict) {
			exhausted++
		} else {
			t.Fatalf("unexpected concurrent budget result: %v", siblingErrs[i])
		}
	}
	if passed != 2 || exhausted != 1 {
		t.Fatalf("concurrent overspend: passed=%d exhausted=%d", passed, exhausted)
	}
	current, err = s.GetToolParent(ctx, subject, session, parent.OperationID)
	current = must(t, current, err)
	if current.SearchRemaining != 1 || current.ReadRemaining != 6 || current.QuoteRemaining != 16374 {
		t.Fatalf("concurrent reservations did not serialize: %+v", current)
	}
	// A fresh key sees the newer manual publication; old parent still pins its
	// original snapshot even after the pointer moves.
	r2 := release(t, s, f.module, []string{f.source.RevisionId}, nil, "later-tool-release")
	b2 := testenv.Ready(t, s, build(t, s, r2, "later-tool-build"), r2)
	activate(t, s, f.module, r2, b2, 1)
	replay, err := s.ReserveToolParent(ctx, subject, session, "tool-parent-key-1", f.module.Id)
	replay = must(t, replay, err)
	if replay.Snapshot.ReleaseId != f.release.ReleaseId {
		t.Fatal("parent key reselected current publication")
	}
	fresh, err := s.ReserveToolParent(ctx, subject, session, "tool-parent-key-2", f.module.Id)
	fresh = must(t, fresh, err)
	if fresh.Snapshot.ReleaseId != r2.ReleaseId {
		t.Fatal("fresh parent did not select current publication")
	}
}

func TestToolSchemaMigrationProbe(t *testing.T) {
	s := testenv.Store(t)
	if err := s.CheckToolSearchSchema(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := s.DB.Exec(ctx, "DROP TABLE knowledge_tool_reads")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CheckToolSearchSchema(ctx); err == nil {
		t.Fatal("missing Tool table passed preflight")
	}
	raw, err := os.ReadFile("../../../scripts/migrate-tool-search.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, string(raw), pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatalf("operator Tool migration failed on existing schema: %v", err)
	}
	if err = s.CheckToolSearchSchema(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestToolVerifiedEvidenceAndDurableReread(t *testing.T) {
	s := testenv.Store(t)
	f := makeCitationFixture(t, s)
	subject := types.AcceptedSubjectRef{AuthorityId: "rtw.identity", TenantId: "platform", SubjectId: "9123"}
	parent, err := s.ReserveToolParent(ctx, subject, "tool-cited-session", "tool-cited-parent-key", f.module.Id)
	parent = must(t, parent, err)
	child, err := s.ReserveToolSearch(ctx, parent, "tool-cited-search-key", model.ToolSearchInput{
		Query: "Where is the citation?", Depth: "fast", Intelligence: "low", ReadCalls: 8, QuoteRunes: 8192})
	child = must(t, child, err)
	child, yes, err := s.ClaimToolSearch(ctx, child)
	child = must(t, child, err)
	if !yes {
		t.Fatal("child was not claimed")
	}
	req := makeCitationRequest(t, f, child.SearchID)
	var pack map[string]any
	if err = json.Unmarshal([]byte(req.PackJson), &pack); err != nil {
		t.Fatal(err)
	}
	pack["profile"] = map[string]any{"requested_depth": "fast", "effective_depth": "fast",
		"requested_intelligence": "low", "effective_intelligence": "low"}
	pack["stop_reason"] = "batch_complete"
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	req.PackJson, req.PackHash = string(raw), object.Hash(raw)
	receipt, err := s.AcceptSearchCitations(ctx, req)
	receipt = must(t, receipt, err)
	var body struct {
		Evidence []struct {
			EvidenceID string `json:"evidence_id"`
		} `json:"evidence"`
	}
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	result := types.ToolSearchResult{SearchId: child.SearchID, Status: "complete", StopReason: "batch_complete",
		SnapshotRef: parent.SnapshotRef, RequestedIntelligence: "low", EffectiveIntelligence: "low",
		Evidence: []types.ToolEvidence{{EvidenceId: body.Evidence[0].EvidenceID,
			RevisionId: f.chunk.RevisionId, Locator: f.chunk.Location.Locator,
			Quote: f.chunk.Text, QuoteHash: f.chunk.TextHash, SourceKind: f.chunk.SourceKind}},
		Gaps: []string{}, Conflicts: []string{}, PackHash: req.PackHash,
		CitationReceipt: &types.ToolReceipt{SearchId: child.SearchID, PackHash: req.PackHash,
			DurableRef: receipt.DurableRef},
		Usage: types.ToolUsage{ReadCalls: 1, QuoteRunes: len([]rune(f.chunk.Text))}}
	if err = s.VerifyToolSearchResult(ctx, parent, child, result, false); err != nil {
		t.Fatal(err)
	}
	forged := result
	forged.Evidence = append([]types.ToolEvidence(nil), result.Evidence...)
	forged.Evidence[0].Quote = "forged"
	if err = s.VerifyToolSearchResult(ctx, parent, child, forged, false); !errors.Is(err, model.ErrArtifactUnavailable) {
		t.Fatalf("forged quote passed RTW exact pack verification: %v", err)
	}
	missingArray := result
	missingArray.Conflicts = nil
	if err = s.VerifyToolSearchResult(ctx, parent, child, missingArray, false); !errors.Is(err, model.ErrArtifactUnavailable) {
		t.Fatalf("nullable conflicts escaped WhaleHall product contract: %v", err)
	}
	resultRaw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteToolSearch(ctx, parent, child, resultRaw, 1, len([]rune(f.chunk.Text)), false); err != nil {
		t.Fatal(err)
	}
	read, err := s.ReadToolEvidence(ctx, parent, "tool-cited-read-key", child.SearchID, body.Evidence[0].EvidenceID)
	read = must(t, read, err)
	if read.Evidence.Quote != f.chunk.Text || read.CitationReceipt.DurableRef != receipt.DurableRef ||
		read.SnapshotRef != parent.SnapshotRef {
		t.Fatalf("reread differs from RTW original: %+v", read)
	}
	replayed, err := s.ReadToolEvidence(ctx, parent, "tool-cited-read-key", child.SearchID, body.Evidence[0].EvidenceID)
	replayed = must(t, replayed, err)
	if !reflect.DeepEqual(replayed, read) {
		t.Fatal("read replay changed evidence")
	}
	_, err = s.ReadToolEvidence(ctx, parent, "tool-cited-read-key", child.SearchID, "ev_another")
	expectError(t, err, model.ErrNotFound)
	current, err := s.GetToolParent(ctx, subject, parent.SessionID, parent.OperationID)
	current = must(t, current, err)
	if current.SearchRemaining != 3 || current.ReadRemaining != 22 ||
		current.QuoteRemaining != model.ToolMaxQuoteRunes-2*len([]rune(f.chunk.Text)) {
		t.Fatalf("search and read usage not accounted exactly once: %+v", current)
	}
	_, err = s.Withdraw(ctx, "admin", types.WithdrawReq{ModuleId: f.module.Id,
		TargetKind: "revision", TargetId: f.source.RevisionId, Reason: "withdraw after Tool read",
		IdempotencyKey: "tool-cited-withdraw"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ReadToolEvidence(ctx, parent, "tool-cited-read-key", child.SearchID, body.Evidence[0].EvidenceID)
	expectError(t, err, model.ErrUnavailable)
	if err = s.VerifyToolSearchResult(ctx, parent, child, result, false); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("withdrawn quote remained public: %v", err)
	}
}
