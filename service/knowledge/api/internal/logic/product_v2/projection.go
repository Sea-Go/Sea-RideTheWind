package product_v2

import (
	"sea-try-go/service/knowledge/api/internal/types"
	"sea-try-go/service/user/user/identity"
)

// The authenticated RTW source retains its frozen legacy storage slot. The
// v2 product wire exposes only the stable issuer and canonical UID.
func legacySubject(subject identity.SubjectRef) types.AcceptedSubjectRef {
	return types.AcceptedSubjectRef{AuthorityId: subject.AuthorityID,
		TenantId: subject.TenantID, SubjectId: subject.SubjectID}
}

func projectAnswer(answer types.AcceptedAnswer) types.AcceptedAnswerV2 {
	return types.AcceptedAnswerV2{
		AnswerId: answer.AnswerId, SearchId: answer.SearchId,
		Subject:   types.AcceptedSubjectRefV2{Issuer: answer.Subject.AuthorityId, SubjectId: answer.Subject.SubjectId},
		SessionId: answer.SessionId, Status: answer.Status,
		AcceptedOrdinal: answer.AcceptedOrdinal, AcceptedAt: answer.AcceptedAt,
		TurnJson: answer.TurnJson,
	}
}
