// Package identity keeps the User Center's existing private import path while
// the authoritative resolver is shared with other RTW services.
package identity

import (
	"context"

	shared "sea-try-go/service/user/user/identity"
	"sea-try-go/service/user/user/rpc/pb"
)

var (
	ErrInvalidClaim      = shared.ErrInvalidClaim
	ErrUserNotFound      = shared.ErrUserNotFound
	ErrIdentityMismatch  = shared.ErrIdentityMismatch
	ErrUserInactive      = shared.ErrUserInactive
	ErrStatusUnavailable = shared.ErrStatusUnavailable
)

const (
	AuthorityID      = shared.AuthorityID
	PlatformTenantID = shared.PlatformTenantID
)

type SubjectRef shared.SubjectRef
type UserReader = shared.UserReader

func ClaimedUID(ctx context.Context) (int64, error) { return shared.ClaimedUID(ctx) }
func ResolveUser(ctx context.Context, users UserReader) (*pb.UserInfo, error) {
	return shared.ResolveUser(ctx, users)
}
func ResolveSubjectRef(ctx context.Context, users UserReader) (SubjectRef, error) {
	ref, err := shared.ResolveSubjectRef(ctx, users)
	return SubjectRef(ref), err
}
