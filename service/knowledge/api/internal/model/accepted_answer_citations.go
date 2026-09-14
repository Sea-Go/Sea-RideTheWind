package model

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/types"
)

// GetProductAnswerCitationStates reads only the references actually cited by
// one accepted answer. Historical answer text remains immutable; this product
// projection reports whether its fixed release is still available now and
// never republishes the old quote from the stored EvidencePack.
func (s *Store) GetProductAnswerCitationStates(ctx context.Context, subject types.AcceptedSubjectRef,
	sessionID, answerID string) (types.ProductAnswerCitationStates, error) {
	answer, err := s.GetAcceptedAnswer(ctx, subject, sessionID, answerID)
	if err != nil {
		return types.ProductAnswerCitationStates{}, err
	}
	out := types.ProductAnswerCitationStates{AnswerId: answer.AnswerId, SearchId: answer.SearchId,
		Status: answer.Status, Citations: []types.SearchCitationReference{}}
	if answer.Status == "insufficient" {
		return out, nil
	}
	if answer.Status != "succeeded" {
		return types.ProductAnswerCitationStates{}, ErrArtifactUnavailable
	}
	rows, err := s.DB.Query(ctx, `SELECT evidence_id FROM knowledge_answer_citations
 WHERE answer_id=$1 ORDER BY citation_order ASC`, answerID)
	if err != nil {
		return types.ProductAnswerCitationStates{}, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return types.ProductAnswerCitationStates{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return types.ProductAnswerCitationStates{}, err
	}
	rows.Close()
	if len(ids) == 0 || len(ids) > 100 {
		return types.ProductAnswerCitationStates{}, ErrArtifactUnavailable
	}
	record, err := s.GetSearchCitations(ctx, answer.SearchId)
	if err != nil {
		return types.ProductAnswerCitationStates{}, err
	}
	byID := make(map[string]types.SearchCitationReference, len(record.Evidence))
	for _, ref := range record.Evidence {
		if ref.EvidenceId == "" || byID[ref.EvidenceId].EvidenceId != "" {
			return types.ProductAnswerCitationStates{}, ErrArtifactUnavailable
		}
		byID[ref.EvidenceId] = ref
	}
	for _, id := range ids {
		ref, ok := byID[id]
		if !ok {
			return types.ProductAnswerCitationStates{}, ErrArtifactUnavailable
		}
		out.Citations = append(out.Citations, ref)
	}
	out.ModuleId, out.ReleaseId, out.PublicationRevision = record.ModuleId, record.ReleaseId, record.PublicationRevision
	return out, nil
}
