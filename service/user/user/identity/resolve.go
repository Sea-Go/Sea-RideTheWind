// Package identity resolves a user from the authenticated usercenter JWT and
// the authoritative UserService into RTW's single-platform subject namespace.
package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"sea-try-go/service/user/user/rpc/pb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrInvalidClaim     = errors.New("invalid authenticated user claim")
	ErrUserNotFound     = errors.New("authenticated user not found")
	ErrIdentityMismatch = errors.New("authenticated user RPC identity mismatch")
)

const (
	// AuthorityID is RTW's stable source for user identities.
	AuthorityID = "rtw.identity"
	// PlatformTenantID is a namespace for this single-platform deployment.
	// It is not an organization or a claim obtained from the client.
	PlatformTenantID = "platform"
)

// SubjectRef is the RTW-issued H01 wire value. The three fields are generated
// by this service from a verified user, never decoded from a client payload.
type SubjectRef struct {
	AuthorityID string `json:"authority_id"`
	TenantID    string `json:"tenant_id"`
	SubjectID   string `json:"subject_id"`
}

// UserReader is the narrow authoritative read needed by identity resolution.
// Production passes the existing UserService gRPC client.
type UserReader interface {
	GetUser(context.Context, *pb.GetUserReq, ...grpc.CallOption) (*pb.GetUserResp, error)
}

// ClaimedUID accepts only the userId value installed by go-zero's verified JWT
// middleware. A UID in a request body, header, or client event is never used.
func ClaimedUID(ctx context.Context) (int64, error) {
	if ctx == nil {
		return 0, ErrInvalidClaim
	}
	claim, ok := ctx.Value("userId").(json.Number)
	if !ok {
		return 0, ErrInvalidClaim
	}
	uid, err := claim.Int64()
	if err != nil || uid <= 0 {
		return 0, ErrInvalidClaim
	}
	return uid, nil
}

// ResolveUser verifies that the authenticated UID still names the same user
// in RTW. It must be called only behind go-zero's JWT middleware.
func ResolveUser(ctx context.Context, users UserReader) (*pb.UserInfo, error) {
	uid, err := ClaimedUID(ctx)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return nil, fmt.Errorf("user RPC is nil: %w", ErrIdentityMismatch)
	}
	response, err := users.GetUser(ctx, &pb.GetUserReq{Uid: uid})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, fmt.Errorf("user RPC: %w", ErrUserNotFound)
		}
		return nil, err
	}
	if response == nil || !response.Found || response.User == nil {
		return nil, ErrUserNotFound
	}
	if response.User.Uid != uid {
		return nil, ErrIdentityMismatch
	}
	return response.User, nil
}

// ResolveSubjectRef issues a complete source-side SubjectRef in RTW's current
// single-platform namespace. A future multi-tenant product needs a new
// authoritative mapping contract before this value can be changed.
func ResolveSubjectRef(ctx context.Context, users UserReader) (SubjectRef, error) {
	user, err := ResolveUser(ctx, users)
	if err != nil {
		return SubjectRef{}, err
	}
	return SubjectRef{
		AuthorityID: AuthorityID,
		TenantID:    PlatformTenantID,
		SubjectID:   strconv.FormatInt(user.Uid, 10),
	}, nil
}
