package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	rtwjwt "sea-try-go/service/user/common/jwt"
	"sea-try-go/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/rest/handler"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type userReaderStub struct {
	called int
	want   int64
	got    int64
	result *pb.GetUserResp
	err    error
}

func (s *userReaderStub) GetUser(_ context.Context, req *pb.GetUserReq, _ ...grpc.CallOption) (*pb.GetUserResp, error) {
	s.called++
	s.got = req.Uid
	return s.result, s.err
}

func TestResolveUserFromVerifiedGoZeroJWT(t *testing.T) {
	const secret = "test-only-jwt-secret-with-enough-length"
	issued, err := rtwjwt.GetToken(secret, time.Now().Unix(), 60, 9123)
	if err != nil {
		t.Fatal(err)
	}
	reader := &userReaderStub{want: 9123, result: &pb.GetUserResp{Found: true, User: &pb.UserInfo{Uid: 9123, Username: "member"}}}
	var resolved *pb.UserInfo
	var resolveErr error
	h := handler.Authorize(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolved, resolveErr = ResolveUser(r.Context(), reader)
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/user/get", nil)
	req.Header.Set("Authorization", "Bearer "+issued)
	req.Header.Set("X-User-ID", "666") // Client headers cannot override verified JWT.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent || resolveErr != nil || resolved == nil || resolved.Uid != 9123 || reader.called != 1 || reader.got != reader.want {
		t.Fatalf("verified resolution: HTTP=%d user=%+v error=%v reader=%+v", w.Code, resolved, resolveErr, reader)
	}

	reader.called = 0
	bad := httptest.NewRequest(http.MethodGet, "/user/get", nil)
	bad.Header.Set("Authorization", "Bearer invalid")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, bad)
	if w.Code != http.StatusUnauthorized || reader.called != 0 {
		t.Fatalf("invalid JWT reached RPC: HTTP=%d calls=%d", w.Code, reader.called)
	}
}

func TestResolveUserRejectsInvalidOrMismatchedIdentity(t *testing.T) {
	valid := context.WithValue(context.Background(), "userId", json.Number("9123"))
	for _, claim := range []any{nil, int64(9123), json.Number("0"), json.Number("-1"), json.Number("bad"), json.Number("9223372036854775808")} {
		ctx := context.WithValue(context.Background(), "userId", claim)
		stub := &userReaderStub{}
		if _, err := ResolveUser(ctx, stub); !errors.Is(err, ErrInvalidClaim) || stub.called != 0 {
			t.Fatalf("claim %v: error=%v RPC calls=%d", claim, err, stub.called)
		}
	}
	for _, tc := range []struct {
		name string
		resp *pb.GetUserResp
		err  error
		want error
	}{
		{"not-found-rpc", nil, status.Error(codes.NotFound, "missing"), ErrUserNotFound},
		{"not-found-response", &pb.GetUserResp{Found: false}, nil, ErrUserNotFound},
		{"nil-response", nil, nil, ErrUserNotFound},
		{"nil-user", &pb.GetUserResp{Found: true}, nil, ErrUserNotFound},
		{"mismatched-uid", &pb.GetUserResp{Found: true, User: &pb.UserInfo{Uid: 7777}}, nil, ErrIdentityMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &userReaderStub{result: tc.resp, err: tc.err}
			if _, err := ResolveUser(valid, stub); !errors.Is(err, tc.want) || stub.called != 1 || stub.got != 9123 {
				t.Fatalf("error=%v RPC calls=%d UID=%d", err, stub.called, stub.got)
			}
		})
	}
	upstream := status.Error(codes.Unavailable, "unavailable")
	stub := &userReaderStub{err: upstream}
	if _, err := ResolveUser(valid, stub); !errors.Is(err, upstream) {
		t.Fatalf("upstream error not preserved: %v", err)
	}
}
