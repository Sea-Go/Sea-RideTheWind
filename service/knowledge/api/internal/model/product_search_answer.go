package model

import (
	"context"
	"encoding/json"
	"reflect"

	"sea-try-go/service/knowledge/api/internal/types"
)

// VerifiedProductSearch projects only an RTW-accepted root turn bound to the
// operation's fixed subject, session, request and publication. A BTW HTTP 200
// cannot create or alter this result.
func (s *Store) VerifiedProductSearch(ctx context.Context, op ProductSearchOperation) (types.ProductSearchResult, error) {
	answer, err := s.GetAcceptedAnswer(ctx, op.Subject, op.SessionID, op.AnswerID)
	if err != nil {
		return types.ProductSearchResult{}, err
	}
	if answer.SearchId != op.SearchID || (answer.Status != "succeeded" && answer.Status != "insufficient") {
		return types.ProductSearchResult{}, ErrArtifactUnavailable
	}
	var turn acceptedRootTurn
	if err = json.Unmarshal([]byte(answer.TurnJson), &turn); err != nil {
		return types.ProductSearchResult{}, ErrArtifactUnavailable
	}
	var fixed citationSnapshot
	raw, err := json.Marshal(op.Snapshot)
	if err != nil {
		return types.ProductSearchResult{}, err
	}
	if err = json.Unmarshal(raw, &fixed); err != nil {
		return types.ProductSearchResult{}, err
	}
	if turn.Request.SearchID != op.SearchID || turn.Request.AnswerID != op.AnswerID ||
		turn.Request.Subject != op.Subject || turn.Request.SessionID != op.SessionID ||
		turn.Request.Search.Query != op.Search.Query || turn.Request.Search.Depth != op.Search.Depth ||
		turn.Request.Search.Intelligence != op.Search.Intelligence ||
		!reflect.DeepEqual(turn.Request.Search.Snapshot, fixed) ||
		turn.Result.AnswerID != op.AnswerID || turn.Result.SummaryStatus != answer.Status {
		return types.ProductSearchResult{}, ErrArtifactUnavailable
	}
	var pack citationPack
	if err = json.Unmarshal(turn.Result.Search.Pack, &pack); err != nil || pack.SearchID != op.SearchID ||
		!reflect.DeepEqual(pack.Snapshot, fixed) {
		return types.ProductSearchResult{}, ErrArtifactUnavailable
	}
	result := types.ProductSearchResult{SearchId: op.SearchID, AnswerId: op.AnswerID,
		Status: answer.Status, Answer: turn.Result.Answer, Citations: []types.ProductSearchCitation{}}
	if answer.Status == "insufficient" {
		if result.Answer != "" || len(turn.Result.Citations) != 0 || len(pack.Evidence) != 0 ||
			turn.Result.Search.Receipt != (types.SearchCitationReceipt{}) {
			return types.ProductSearchResult{}, ErrArtifactUnavailable
		}
		return result, nil
	}
	states, err := s.GetProductAnswerCitationStates(ctx, op.Subject, op.SessionID, op.AnswerID)
	if err != nil {
		return types.ProductSearchResult{}, err
	}
	record, err := s.GetSearchCitations(ctx, op.SearchID)
	if err != nil {
		return types.ProductSearchResult{}, err
	}
	if record.DurableRef != turn.Result.Search.Receipt.DurableRef ||
		record.PackHash != turn.Result.Search.Receipt.PackHash ||
		states.ModuleId != fixed.ModuleID || states.ReleaseId != fixed.ReleaseID ||
		states.PublicationRevision != fixed.PublicationRevision ||
		len(states.Citations) != len(turn.Result.Citations) {
		return types.ProductSearchResult{}, ErrArtifactUnavailable
	}
	byID := make(map[string]citationEvidence, len(pack.Evidence))
	for _, evidence := range pack.Evidence {
		byID[evidence.ID] = evidence
	}
	for i, ref := range states.Citations {
		evidence, ok := byID[ref.EvidenceId]
		if !ok || ref.EvidenceId != turn.Result.Citations[i] || ref.State != "available" ||
			ref.SourceKind != evidence.Key.SourceKind || ref.ContentId != evidence.Key.ContentID ||
			ref.RevisionId != evidence.Key.RevisionID || ref.ChunkId != evidence.Key.ChunkID ||
			ref.Original != evidence.Original || ref.Locator != evidence.Locator ||
			ref.QuoteHash != evidence.QuoteHash {
			return types.ProductSearchResult{}, ErrArtifactUnavailable
		}
		result.Citations = append(result.Citations, types.ProductSearchCitation{
			EvidenceId: evidence.ID, SourceKind: evidence.Key.SourceKind, ContentId: evidence.Key.ContentID,
			RevisionId: evidence.Key.RevisionID, Locator: evidence.Locator,
			Original: evidence.Original, Quote: evidence.Quote, QuoteHash: evidence.QuoteHash,
		})
	}
	result.CitationReceiptRef = record.DurableRef
	return result, nil
}
