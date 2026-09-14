package logic

import (
	"context"
	"testing"

	"sea-try-go/service/favorite/rpc/internal/svc"
	"sea-try-go/service/user/user/rpc/userservice"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type favoriteUserReader struct {
	userservice.UserService
	uid    int64
	status *int64
}

func (u favoriteUserReader) GetUser(context.Context, *userservice.GetUserReq, ...grpc.CallOption) (*userservice.GetUserResp, error) {
	return &userservice.GetUserResp{Found: true,
		User: &userservice.UserInfo{Uid: u.uid, Status: u.status}}, nil
}

func TestFavoriteFactRequiresActiveMatchingUID(t *testing.T) {
	active, banned := int64(0), int64(1)
	for _, test := range []struct {
		name   string
		reader favoriteUserReader
		want   codes.Code
	}{
		{"active", favoriteUserReader{uid: 1001, status: &active}, codes.OK},
		{"banned", favoriteUserReader{uid: 1001, status: &banned}, codes.PermissionDenied},
		{"old_rpc_missing_status", favoriteUserReader{uid: 1001}, codes.Unavailable},
		{"wrong_uid", favoriteUserReader{uid: 1002, status: &active}, codes.Unavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ensureUserExists(context.Background(), &svc.ServiceContext{UserRpc: test.reader}, 1001)
			if status.Code(err) != test.want {
				t.Fatalf("active UID gate: got %v, want %v", err, test.want)
			}
		})
	}
}
