package model

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

func toolPackSnapshot(snapshot types.SearchSnapshot) (citationSnapshot, error) {
	var fixed citationSnapshot
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return fixed, err
	}
	err = json.Unmarshal(raw, &fixed)
	return fixed, err
}

func toolEvidenceFromPack(e citationEvidence) types.ToolEvidence {
	return types.ToolEvidence{EvidenceId: e.ID, RevisionId: e.Key.RevisionID,
		Locator: e.Locator.Locator, Quote: e.Quote, QuoteHash: e.QuoteHash,
		SourceKind: e.Key.SourceKind}
}

// VerifyToolSearchResult rejects a BTW transport reply unless it matches RTW's
// exact durable citation pack. A successful HTTP response cannot mint a quote.
func (s *Store) VerifyToolSearchResult(ctx context.Context, parent ToolParent, child ToolSearch,
	result types.ToolSearchResult, allowLower bool) error {
	if result.SearchId != child.SearchID || result.SnapshotRef != parent.SnapshotRef ||
		result.RequestedIntelligence != child.Input.Intelligence ||
		strings.TrimSpace(result.StopReason) == "" || len(result.StopReason) > 256 ||
		result.Evidence == nil || result.Gaps == nil || result.Conflicts == nil ||
		result.Usage.ReadCalls < len(result.Evidence) || result.Usage.ReadCalls > child.ReservedReads ||
		result.Usage.QuoteRunes < 0 || result.Usage.QuoteRunes > child.ReservedRunes ||
		len(result.Evidence) > 100 || len(result.Gaps) > 100 || len(result.Conflicts) != 0 ||
		(result.Status != "complete" && result.Status != "partial" && result.Status != "empty") {
		return ErrArtifactUnavailable
	}
	levels := map[string]int{"low": 0, "medium": 1, "high": 2}
	effective, ok := levels[result.EffectiveIntelligence]
	requested := levels[child.Input.Intelligence]
	if !ok || effective > requested || (effective != requested && !allowLower) ||
		(result.Status == "complete" && len(result.Gaps) != 0) {
		return ErrArtifactUnavailable
	}
	runes := 0
	for _, e := range result.Evidence {
		runes += utf8.RuneCountInString(e.Quote)
	}
	if runes > result.Usage.QuoteRunes {
		return ErrArtifactUnavailable
	}
	if result.Status == "empty" {
		if len(result.Evidence) != 0 || result.PackHash != "" || result.CitationReceipt != nil {
			return ErrArtifactUnavailable
		}
		// A real empty result has no RTW citation row. A previously accepted row
		// for this fixed search ID is a conflict, never an empty projection.
		_, _, err := s.citationReceipt(ctx, child.SearchID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err == nil {
			return ErrArtifactUnavailable
		}
		return err
	}
	if len(result.Evidence) == 0 || !citationHash(result.PackHash) || result.CitationReceipt == nil ||
		result.CitationReceipt.SearchId != child.SearchID ||
		result.CitationReceipt.PackHash != result.PackHash || result.CitationReceipt.DurableRef == "" {
		return ErrArtifactUnavailable
	}
	receipt, raw, err := s.citationReceipt(ctx, child.SearchID)
	if err != nil {
		return err
	}
	if receipt.PackHash != result.PackHash || receipt.DurableRef != result.CitationReceipt.DurableRef {
		return ErrArtifactUnavailable
	}
	var pack citationPack
	if err = decodeCitationJSON([]byte(raw), &pack); err != nil {
		return ErrArtifactUnavailable
	}
	fixed, err := toolPackSnapshot(parent.Snapshot)
	if err != nil {
		return err
	}
	var profile struct {
		RequestedDepth        string `json:"requested_depth"`
		EffectiveDepth        string `json:"effective_depth"`
		RequestedIntelligence string `json:"requested_intelligence"`
		EffectiveIntelligence string `json:"effective_intelligence"`
	}
	if err = json.Unmarshal(pack.Profile, &profile); err != nil {
		return ErrArtifactUnavailable
	}
	if pack.SearchID != child.SearchID || !reflect.DeepEqual(pack.Snapshot, fixed) ||
		pack.Status != result.Status || pack.StopReason != result.StopReason ||
		!reflect.DeepEqual(pack.Gaps, result.Gaps) || len(pack.Evidence) != len(result.Evidence) ||
		profile.RequestedDepth != child.Input.Depth || profile.EffectiveDepth != child.Input.Depth ||
		profile.RequestedIntelligence != result.RequestedIntelligence ||
		profile.EffectiveIntelligence != result.EffectiveIntelligence {
		return ErrArtifactUnavailable
	}
	for i, e := range pack.Evidence {
		if toolEvidenceFromPack(e) != result.Evidence[i] {
			return ErrArtifactUnavailable
		}
	}
	// A concurrent withdrawal must not yield a fresh public quote. The
	// historical citation row remains, but every reference becomes unavailable.
	states, err := s.GetSearchCitations(ctx, child.SearchID)
	if err != nil {
		return err
	}
	if len(states.Evidence) != len(result.Evidence) {
		return ErrArtifactUnavailable
	}
	for _, state := range states.Evidence {
		if state.State != "available" {
			return ErrUnavailable
		}
	}
	return nil
}

// ReadToolEvidence uses RTW's immutable accepted pack and exact source reader.
// It spends one parent read and the returned quote length once per idempotency
// key; BTW and model JSON cannot select a newer revision or another parent.
func (s *Store) ReadToolEvidence(ctx context.Context, parent ToolParent, key, searchID, evidenceID string) (types.ToolReadResult, error) {
	if !validToolKey(key) || !citationIdentity(searchID) || !citationIdentity(evidenceID) {
		return types.ToolReadResult{}, invalid("bounded evidence read IDs and key required")
	}
	child, err := s.GetToolSearch(ctx, parent, searchID)
	if err != nil {
		return types.ToolReadResult{}, err
	}
	if child.Status != "complete" {
		return types.ToolReadResult{}, ErrConflict
	}
	var projected types.ToolSearchResult
	if err = json.Unmarshal(child.Result, &projected); err != nil || projected.SearchId != searchID {
		return types.ToolReadResult{}, ErrArtifactUnavailable
	}
	var authorized bool
	for _, e := range projected.Evidence {
		if e.EvidenceId == evidenceID {
			authorized = true
			break
		}
	}
	if !authorized {
		return types.ToolReadResult{}, ErrNotFound
	}
	receipt, raw, err := s.citationReceipt(ctx, searchID)
	if err != nil {
		return types.ToolReadResult{}, err
	}
	if projected.CitationReceipt == nil || projected.CitationReceipt.PackHash != receipt.PackHash ||
		projected.CitationReceipt.DurableRef != receipt.DurableRef {
		return types.ToolReadResult{}, ErrArtifactUnavailable
	}
	var pack citationPack
	if err = decodeCitationJSON([]byte(raw), &pack); err != nil {
		return types.ToolReadResult{}, ErrArtifactUnavailable
	}
	fixed, err := toolPackSnapshot(parent.Snapshot)
	if err != nil {
		return types.ToolReadResult{}, err
	}
	if !reflect.DeepEqual(pack.Snapshot, fixed) {
		return types.ToolReadResult{}, ErrArtifactUnavailable
	}
	var selected *citationEvidence
	for i := range pack.Evidence {
		if pack.Evidence[i].ID == evidenceID {
			selected = &pack.Evidence[i]
			break
		}
	}
	if selected == nil {
		return types.ToolReadResult{}, ErrArtifactUnavailable
	}
	result := types.ToolReadResult{SearchId: searchID, SnapshotRef: parent.SnapshotRef,
		Evidence: toolEvidenceFromPack(*selected), CitationReceipt: types.ToolReceipt{
			SearchId: searchID, PackHash: receipt.PackHash, DurableRef: receipt.DurableRef}}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return types.ToolReadResult{}, err
	}
	defer tx.Rollback(context.Background())
	var readRemaining, quoteRemaining int
	var live bool
	err = tx.QueryRow(ctx, `SELECT expires_at>clock_timestamp(),read_remaining,quote_remaining FROM knowledge_tool_parents
 WHERE operation_id=$1 AND authority_id=$2 AND tenant_id=$3 AND subject_id=$4 AND session_id=$5 FOR UPDATE`,
		parent.OperationID, parent.Subject.AuthorityId, parent.Subject.TenantId, parent.Subject.SubjectId,
		parent.SessionID).Scan(&live, &readRemaining, &quoteRemaining)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.ToolReadResult{}, ErrNotFound
	}
	if err != nil {
		return types.ToolReadResult{}, err
	}
	// Publication/withdrawal writers take this lock first. Holding it until
	// commit makes the same-generation reread valid at the response boundary.
	if _, err = tx.Exec(ctx, `SELECT 1 FROM knowledge_modules WHERE id=$1 FOR UPDATE`, fixed.ModuleID); err != nil {
		return types.ToolReadResult{}, err
	}
	states, err := s.GetSearchCitations(ctx, searchID)
	if err != nil {
		return types.ToolReadResult{}, err
	}
	for _, state := range states.Evidence {
		if state.State != "available" {
			return types.ToolReadResult{}, ErrUnavailable
		}
	}
	if !live {
		return types.ToolReadResult{}, ErrUnavailable
	}
	var oldSearch, oldEvidence string
	var oldRaw []byte
	err = tx.QueryRow(ctx, `SELECT search_id,evidence_id,result_json FROM knowledge_tool_reads
 WHERE operation_id=$1 AND operation_key=$2`, parent.OperationID, key).Scan(&oldSearch, &oldEvidence, &oldRaw)
	if err == nil {
		if oldSearch != searchID || oldEvidence != evidenceID {
			return types.ToolReadResult{}, conflictCode("IDEMPOTENCY_CONFLICT", "Tool read key reused")
		}
		var previous types.ToolReadResult
		if json.Unmarshal(oldRaw, &previous) != nil {
			return types.ToolReadResult{}, ErrArtifactUnavailable
		}
		return previous, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return types.ToolReadResult{}, err
	}
	runes := utf8.RuneCountInString(selected.Quote)
	if readRemaining < 1 || quoteRemaining < runes {
		return types.ToolReadResult{}, conflictCode("BUDGET_EXHAUSTED", "Tool parent cumulative read budget exhausted")
	}
	// Source recheck is done while the parent lock is held so a concurrent
	// second read cannot overspend the same remaining budget.
	chunk, err := s.ReadSearchSource(ctx, types.ReadSearchSourceReq{ModuleId: fixed.ModuleID,
		ReleaseId: fixed.ReleaseID, Generation: fixed.Generation,
		PublicationRevision: fixed.PublicationRevision, RevisionId: selected.Key.RevisionID,
		ChunkId: selected.Key.ChunkID})
	if err != nil {
		return types.ToolReadResult{}, err
	}
	if chunk.Text != selected.Quote || chunk.TextHash != selected.QuoteHash || chunk.Location != selected.Locator ||
		chunk.SourceKind != selected.Key.SourceKind {
		return types.ToolReadResult{}, ErrArtifactUnavailable
	}
	resultRaw, err := json.Marshal(result)
	if err != nil {
		return types.ToolReadResult{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO knowledge_tool_reads
 (operation_id,operation_key,search_id,evidence_id,result_json) VALUES($1,$2,$3,$4,$5)`,
		parent.OperationID, key, searchID, evidenceID, resultRaw); err != nil {
		return types.ToolReadResult{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE knowledge_tool_parents SET read_remaining=read_remaining-1,
 quote_remaining=quote_remaining-$2 WHERE operation_id=$1`, parent.OperationID, runes); err != nil {
		return types.ToolReadResult{}, err
	}
	return result, tx.Commit(ctx)
}
