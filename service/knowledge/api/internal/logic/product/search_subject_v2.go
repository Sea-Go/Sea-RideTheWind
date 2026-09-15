package product

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"
	"sea-try-go/service/user/user/identity"
)

// A persisted v1 anchor is a compatibility slot, not an issuer. RTW resolves
// the current JWT through the real User RPC again before issuing any v2 wire.
func issuedSearchSubjectV2(ctx context.Context, service *svc.ServiceContext,
	legacy types.AcceptedSubjectRef) (identity.SubjectRefV2, error) {
	issued, err := identity.ResolveSubjectRefV2(ctx, service.UserRpc)
	if err != nil {
		return identity.SubjectRefV2{}, err
	}
	if legacy.AuthorityId != identity.AuthorityID || legacy.TenantId != identity.PlatformTenantID ||
		legacy.SubjectId != issued.SubjectID {
		return identity.SubjectRefV2{}, errSearchUpstream
	}
	return issued, nil
}
