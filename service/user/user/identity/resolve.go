// Package identity resolves a user from the authenticated usercenter JWT and
// the authoritative UserService into RTW's stable identity namespace.
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
	ErrInvalidClaim      = errors.New("invalid authenticated user claim")
	ErrUserNotFound      = errors.New("authenticated user not found")
	ErrIdentityMismatch  = errors.New("authenticated user RPC identity mismatch")
	ErrUserInactive      = errors.New("authenticated user inactive")
	ErrStatusUnavailable = errors.New("authenticated user status unavailable")
)

const (
	// AuthorityID is RTW's stable source for user identities.
	AuthorityID = "rtw.identity"
	// PlatformTenantID is only the fixed v1 compatibility slot used by old DB
	// keys. It is not a tenant, organization or claim obtained from the client.
	PlatformTenantID = "platform"
)

// SubjectRef is the legacy v1 wire value. TenantID is a fixed compatibility
// slot, never an organization or a client-supplied identity field.
type SubjectRef struct {
	AuthorityID string `json:"authority_id"`
	TenantID    string `json:"tenant_id"`
	SubjectID   string `json:"subject_id"`
}

// SubjectRefV2 is issued only from an active User RPC record reached through
// go-zero's verified usercenter JWT claim. It has no compatibility tenant slot.
type SubjectRefV2 struct {
	Issuer    string `json:"issuer"`
	SubjectID string `json:"subject_id"`
}

func ValidSubjectRefV2(ref SubjectRefV2) bool {
	if ref.Issuer != AuthorityID || ref.SubjectID == "" {
		return false
	}
	uid, err := strconv.ParseInt(ref.SubjectID, 10, 64)
	return err == nil && uid > 0 && strconv.FormatInt(uid, 10) == ref.SubjectID
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

// ResolveUser verifies that the authenticated UID still names an active user
// in RTW. It must be called only behind go-zero's JWT middleware. An older RPC
// without status presence cannot establish an authenticated product subject.
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
	if response.User.Status == nil {
		return nil, ErrStatusUnavailable
	}
	if response.User.GetStatus() != 0 {
		return nil, ErrUserInactive
	}
	return response.User, nil
}

// ResolveSubjectRef is the one server-owned v1 adapter for old wire and DB
// keys. A future organization relationship remains separate from identity.
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

func ResolveSubjectRefV2(ctx context.Context, users UserReader) (SubjectRefV2, error) {
	user, err := ResolveUser(ctx, users)
	if err != nil {
		return SubjectRefV2{}, err
	}
	return SubjectRefV2{Issuer: AuthorityID, SubjectID: strconv.FormatInt(user.Uid, 10)}, nil
}
