package identity

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	rtwjwt "sea-try-go/service/user/common/jwt"
	shared "sea-try-go/service/user/user/identity"
	"sea-try-go/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/rest/handler"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func statusPointer(status int64) *int64 { return &status }

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

func TestResolveSubjectRefFromVerifiedGoZeroJWT(t *testing.T) {
	const secret = "test-only-jwt-secret-with-enough-length"
	issued, err := rtwjwt.GetToken(secret, time.Now().Unix(), 60, 9123)
	if err != nil {
		t.Fatal(err)
	}
	reader := &userReaderStub{want: 9123, result: &pb.GetUserResp{Found: true, User: &pb.UserInfo{Uid: 9123, Username: "member", Status: statusPointer(0)}}}
	var resolved SubjectRef
	var resolveErr error
	h := handler.Authorize(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolved, resolveErr = ResolveSubjectRef(r.Context(), reader)
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/test/identity", strings.NewReader(`{"subject_ref":{"authority_id":"evil","tenant_id":"other","subject_id":"666"}}`))
	req.Header.Set("Authorization", "Bearer "+issued)
	req.Header.Set("X-User-ID", "666") // Neither header nor payload can override verified JWT.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent || resolveErr != nil || resolved != (SubjectRef{"rtw.identity", "platform", "9123"}) || reader.called != 1 || reader.got != reader.want {
		t.Fatalf("verified resolution: HTTP=%d user=%+v error=%v reader=%+v", w.Code, resolved, resolveErr, reader)
	}
	wire, err := json.Marshal(resolved)
	if err != nil || string(wire) != `{"authority_id":"rtw.identity","tenant_id":"platform","subject_id":"9123"}` {
		t.Fatalf("SubjectRef wire=%s error=%v", wire, err)
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

func TestResolveSubjectRefV2HasNoTenantOrClientIssuer(t *testing.T) {
	const secret = "test-only-v2-jwt-secret-with-enough-length"
	token, err := rtwjwt.GetToken(secret, time.Now().Unix(), 60, 9123)
	if err != nil {
		t.Fatal(err)
	}
	reader := &userReaderStub{result: &pb.GetUserResp{Found: true,
		User: &pb.UserInfo{Uid: 9123, Status: statusPointer(0)}}}
	var issued shared.SubjectRefV2
	var resolveErr error
	h := handler.Authorize(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issued, resolveErr = shared.ResolveSubjectRefV2(r.Context(), reader)
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/test/v2/identity",
		strings.NewReader(`{"issuer":"other.identity","tenant_id":"organization-x","subject_id":"7777"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-User-ID", "7777")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	wire, err := json.Marshal(issued)
	if w.Code != http.StatusNoContent || resolveErr != nil || reader.called != 1 || reader.got != 9123 ||
		!shared.ValidSubjectRefV2(issued) || err != nil ||
		string(wire) != `{"issuer":"rtw.identity","subject_id":"9123"}` {
		t.Fatalf("v2 issuer escaped User RPC: HTTP=%d wire=%s error=%v calls=%d uid=%d",
			w.Code, wire, resolveErr, reader.called, reader.got)
	}
}

func TestResolveUserRejectsInvalidOrMismatchedIdentity(t *testing.T) {
	valid := context.WithValue(context.Background(), "userId", json.Number("9123"))
	for _, claim := range []any{nil, int64(9123), json.Number("0"), json.Number("-1"), json.Number("bad"), json.Number("9223372036854775808")} {
		ctx := context.WithValue(context.Background(), "userId", claim)
		stub := &userReaderStub{}
		ref, err := ResolveSubjectRef(ctx, stub)
		if !errors.Is(err, ErrInvalidClaim) || stub.called != 0 || ref != (SubjectRef{}) {
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
		{"old-rpc-missing-status", &pb.GetUserResp{Found: true, User: &pb.UserInfo{Uid: 9123}}, nil, ErrStatusUnavailable},
		{"banned", &pb.GetUserResp{Found: true, User: &pb.UserInfo{Uid: 9123, Status: statusPointer(1)}}, nil, ErrUserInactive},
		{"unknown-nonzero-status", &pb.GetUserResp{Found: true, User: &pb.UserInfo{Uid: 9123, Status: statusPointer(2)}}, nil, ErrUserInactive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &userReaderStub{result: tc.resp, err: tc.err}
			ref, err := ResolveSubjectRef(valid, stub)
			if !errors.Is(err, tc.want) || stub.called != 1 || stub.got != 9123 || ref != (SubjectRef{}) {
				t.Fatalf("error=%v RPC calls=%d UID=%d", err, stub.called, stub.got)
			}
		})
	}
	upstream := status.Error(codes.Unavailable, "unavailable")
	stub := &userReaderStub{err: upstream}
	if _, err := ResolveSubjectRef(valid, stub); !errors.Is(err, upstream) {
		t.Fatalf("upstream error not preserved: %v", err)
	}
}

func TestResolveSubjectRefKeepsDecimalUIDPrecision(t *testing.T) {
	ctx := context.WithValue(context.Background(), "userId", json.Number("9223372036854775807"))
	stub := &userReaderStub{result: &pb.GetUserResp{Found: true, User: &pb.UserInfo{Uid: math.MaxInt64, Status: statusPointer(0)}}}
	ref, err := ResolveSubjectRef(ctx, stub)
	if err != nil || ref.SubjectID != "9223372036854775807" || ref.AuthorityID != AuthorityID || ref.TenantID != PlatformTenantID {
		t.Fatalf("large UID resolution=%+v error=%v", ref, err)
	}
}

func TestUserStatusProto3Presence(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   *pb.UserInfo
		want *int64
	}{
		{"old-provider", &pb.UserInfo{Uid: 9123}, nil},
		{"active", &pb.UserInfo{Uid: 9123, Status: statusPointer(0)}, statusPointer(0)},
		{"banned", &pb.UserInfo{Uid: 9123, Status: statusPointer(1)}, statusPointer(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := proto.Marshal(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			var got pb.UserInfo
			if err := proto.Unmarshal(wire, &got); err != nil {
				t.Fatal(err)
			}
			if (got.Status == nil) != (tc.want == nil) || got.GetStatus() != tc.in.GetStatus() {
				t.Fatalf("wire status presence changed: in=%v out=%v", tc.in.Status, got.Status)
			}
		})
	}
}
