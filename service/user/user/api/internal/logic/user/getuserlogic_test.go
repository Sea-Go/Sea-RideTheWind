package user

import (
	"context"
	"encoding/json"
	"testing"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/user/common/errmsg"
	"sea-try-go/service/user/user/api/internal/svc"
	"sea-try-go/service/user/user/api/internal/types"
	"sea-try-go/service/user/user/rpc/pb"
	"sea-try-go/service/user/user/rpc/userservice"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type getUserRPCStub struct {
	userservice.UserService
	called int
	uid    int64
	resp   *pb.GetUserResp
	err    error
}

func (s *getUserRPCStub) GetUser(_ context.Context, req *pb.GetUserReq, _ ...grpc.CallOption) (*pb.GetUserResp, error) {
	s.called++
	s.uid = req.Uid
	return s.resp, s.err
}

func TestGetUserUsesAuthenticatedRPCIdentity(t *testing.T) {
	logger.Init("user-identity-test")
	for _, tc := range []struct {
		name  string
		claim any
		resp  *pb.GetUserResp
		err   error
		code  int
		calls int
	}{
		{"valid", json.Number("9123"), &pb.GetUserResp{Found: true, User: &pb.UserInfo{Uid: 9123, Username: "member"}}, nil, errmsg.Success, 1},
		{"deleted", json.Number("9123"), nil, status.Error(codes.NotFound, "gone"), errmsg.ErrorUserNotExist, 1},
		{"mismatched", json.Number("9123"), &pb.GetUserResp{Found: true, User: &pb.UserInfo{Uid: 9911}}, nil, errmsg.ErrorServerCommon, 1},
		{"zero", json.Number("0"), nil, nil, errmsg.ErrorTokenRuntime, 0},
		{"client-value-not-jwt", int64(9123), nil, nil, errmsg.ErrorTokenRuntime, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rpc := &getUserRPCStub{resp: tc.resp, err: tc.err}
			ctx := context.WithValue(context.Background(), "userId", tc.claim)
			logic := NewGetuserLogic(ctx, &svc.ServiceContext{UserRpc: rpc})
			result, code := logic.Getuser(&types.GetUserReq{})
			if code != tc.code || rpc.called != tc.calls {
				t.Fatalf("code=%d calls=%d, want %d/%d", code, rpc.called, tc.code, tc.calls)
			}
			if tc.calls == 1 && rpc.uid != 9123 {
				t.Fatalf("RPC UID=%d", rpc.uid)
			}
			if tc.code == errmsg.Success {
				if result == nil || !result.Found || result.User.Uid != 9123 {
					t.Fatalf("resolved user=%+v", result)
				}
			} else if result != nil {
				t.Fatalf("failed identity exposed user=%+v", result)
			}
		})
	}
}
