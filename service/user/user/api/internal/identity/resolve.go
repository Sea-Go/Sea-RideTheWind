// Package identity resolves a user from the authenticated usercenter JWT and
// the authoritative UserService. Tenant membership is not represented here.
package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
// in RTW. It intentionally does not issue a SubjectRef: RTW currently has no
// authoritative tenant membership or tenant identifier to put in that value.
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
