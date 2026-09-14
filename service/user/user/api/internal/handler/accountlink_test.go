package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rtwjwt "sea-try-go/service/user/common/jwt"
	"sea-try-go/service/user/user/api/internal/config"
	"sea-try-go/service/user/user/api/internal/svc"
	"sea-try-go/service/user/user/identity/linking"
	"sea-try-go/service/user/user/rpc/pb"
	"sea-try-go/service/user/user/rpc/userservice"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zeromicro/go-zero/rest"
	"google.golang.org/grpc"
)

func TestAccountLinkPostgresConcurrentClaim(t *testing.T) {
	pool := linkTestPool(t)
	store := linking.NewStore(pool)
	dcID := uuid.NewString()
	type result struct {
		link linking.Link
		err  error
	}
	results := make(chan result, 2)
	for _, uid := range []int64{1001, 1002} {
		go func(uid int64) {
			link, err := store.Bind(context.Background(), uid, dcID, 0)
			results <- result{link, err}
		}(uid)
	}
	var winner, conflicts int
	for range 2 {
		got := <-results
		switch {
		case got.err == nil && got.link.State == "active":
			winner++
		case errors.Is(got.err, linking.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent claim: %+v %v", got.link, got.err)
		}
	}
	if winner != 1 || conflicts != 1 {
		t.Fatalf("exactly one DC UUID owner required: winner=%d conflict=%d", winner, conflicts)
	}
	if current, err := store.ActiveByDC(context.Background(), dcID); err != nil ||
		(current.UID != 1001 && current.UID != 1002) || current.Revision != 1 {
		t.Fatalf("single durable DC owner: %+v %v", current, err)
	}
}

type linkTestUsers struct {
	userservice.UserService
	mu       sync.RWMutex
	statuses map[int64]int64
}

func (u *linkTestUsers) GetUser(_ context.Context, req *pb.GetUserReq, _ ...grpc.CallOption) (*pb.GetUserResp, error) {
	u.mu.RLock()
	status, ok := u.statuses[req.Uid]
	u.mu.RUnlock()
	if !ok {
		return &pb.GetUserResp{Found: false}, nil
	}
	return &pb.GetUserResp{Found: true, User: &pb.UserInfo{
		Uid: req.Uid, Status: &status, Email: "same@example.test",
	}}, nil
}

func linkTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("KNOWLEDGE_TEST_DSN")
	if dsn == "" {
		t.Skip("set KNOWLEDGE_TEST_DSN to isolated PostgreSQL via knowledge acceptance script")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "account_link_" + uuid.NewString()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	pc, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pc.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	if _, err := pool.Exec(ctx, linking.MigrationSQL); err != nil {
		t.Fatal(err)
	}
	return pool
}

func linkTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func TestAccountLinkHTTPBindingExchangeAndRevocation(t *testing.T) {
	pool := linkTestPool(t)
	dcA, dcB := uuid.NewString(), uuid.NewString()
	const bearerA, bearerB = "wh_access_link_test_a", "wh_access_link_test_b"
	dcSessions := struct {
		sync.RWMutex
		byToken map[string]string
	}{byToken: map[string]string{"Bearer " + bearerA: dcA, "Bearer " + bearerB: dcB}}
	var dcCalls atomic.Int64
	dc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dcCalls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/v1/auth/me" {
			http.NotFound(w, r)
			return
		}
		dcSessions.RLock()
		id := dcSessions.byToken[r.Header.Get("Authorization")]
		dcSessions.RUnlock()
		if id == "" {
			http.Error(w, "session unavailable", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id,
			"email": "same@example.test", "displayName": "not an identity key"})
	}))
	defer dc.Close()
	verifier, err := linking.NewDCVerifier(dc.URL+"/v1/auth/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "account-link-test-rtw-secret-32-bytes"
	users := &linkTestUsers{statuses: map[int64]int64{1001: 0, 1002: 0}}
	var c config.Config
	c.UserAuth.AccessSecret = secret
	c.RestConf.Host, c.RestConf.Port = "127.0.0.1", linkTestPort(t)
	c.RestConf.Name, c.RestConf.Mode = "account-link-http-test", "test"
	c.RestConf.Timeout = 3000
	service := &svc.ServiceContext{Config: c, UserRpc: users,
		CheckBlacklistMiddleware: func(next http.HandlerFunc) http.HandlerFunc { return next },
		AccountLink: &linking.Service{Store: linking.NewStore(pool), DC: verifier,
			Users: users, JWTSecret: secret}}
	server, err := rest.NewServer(c.RestConf)
	if err != nil {
		t.Fatal(err)
	}
	RegisterAccountLinkHandlers(server, service)
	go server.Start()
	defer server.Stop()
	base := "http://127.0.0.1:" + strconv.Itoa(c.RestConf.Port) + "/usercenter/v1"
	client := &http.Client{Timeout: 3 * time.Second}
	for deadline := time.Now().Add(5 * time.Second); ; {
		res, err := client.Get(base + "/account-link")
		if err == nil {
			_ = res.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("go-zero account-link route did not start: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	rtwA, err := rtwjwt.GetToken(secret, time.Now().Unix(), 3600, 1001)
	if err != nil {
		t.Fatal(err)
	}
	rtwB, err := rtwjwt.GetToken(secret, time.Now().Unix(), 3600, 1002)
	if err != nil {
		t.Fatal(err)
	}
	forgedRTW, err := rtwjwt.GetToken("another-rtw-signing-key", time.Now().Unix(), 3600, 1001)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, rtwToken, dcToken string, body any, out any) int {
		t.Helper()
		var data []byte
		if body != nil {
			data, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequest(method, base+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if rtwToken != "" {
			req.Header.Set("X-RTW-Authorization", "Bearer "+rtwToken)
		}
		if dcToken != "" {
			req.Header.Set("Authorization", "Bearer "+dcToken)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if out != nil {
			if err := json.NewDecoder(res.Body).Decode(out); err != nil {
				t.Fatalf("%s %s status=%d decode=%v", method, path, res.StatusCode, err)
			}
		}
		return res.StatusCode
	}
	linkBody := func(revision int64) map[string]int64 { return map[string]int64{"expected_revision": revision} }
	if status := call(http.MethodPost, "/account-link", "", bearerA, linkBody(0), nil); status != 401 || dcCalls.Load() != 0 {
		t.Fatalf("missing RTW JWT status=%d DC calls=%d", status, dcCalls.Load())
	}
	if status := call(http.MethodPost, "/account-link", forgedRTW, bearerA, linkBody(0), nil); status != 401 || dcCalls.Load() != 0 {
		t.Fatalf("forged RTW JWT status=%d DC calls=%d", status, dcCalls.Load())
	}
	var first linking.Link
	if status := call(http.MethodPost, "/account-link", rtwA, bearerA, linkBody(0), &first); status != 200 || first.UID != 1001 || first.DCUserID != dcA || first.Revision != 1 {
		t.Fatalf("explicit binding status=%d link=%+v", status, first)
	}
	var replay linking.Link
	if status := call(http.MethodPost, "/account-link", rtwA, bearerA, linkBody(0), &replay); status != 200 || replay != first {
		t.Fatalf("same pair replay status=%d link=%+v", status, replay)
	}
	if status := call(http.MethodPost, "/account-link", rtwB, bearerA, linkBody(0), nil); status != 409 {
		t.Fatalf("same DC UUID claimed by other UID: %d", status)
	}
	if status := call(http.MethodPost, "/account-link", rtwA, bearerB, linkBody(1), nil); status != 409 {
		t.Fatalf("active UID silently rebound: %d", status)
	}
	var session linking.ProductSession
	if status := call(http.MethodPost, "/product-sessions/exchange", "", bearerA, nil, &session); status != 200 || session.Token == "" || session.LinkRevision != 1 {
		t.Fatalf("DC bearer exchange status=%d result=%+v", status, session)
	}
	parsed, err := jwt.Parse(session.Token, func(token *jwt.Token) (any, error) { return []byte(secret), nil })
	if err != nil || !parsed.Valid {
		t.Fatalf("RTW product JWT rejected: %v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["userId"] != float64(1001) || claims["exp"].(float64)-claims["iat"].(float64) != float64(linking.ProductTTLSeconds) {
		t.Fatalf("wrong product subject or TTL: %+v", claims)
	}
	dcSessions.Lock()
	dcSessions.byToken["Bearer "+bearerA] = dcB
	dcSessions.Unlock()
	if status := call(http.MethodPost, "/product-sessions/exchange", "", bearerA, nil, nil); status != 404 {
		t.Fatalf("DC bearer switched to unbound account but exchanged: %d", status)
	}
	dcSessions.Lock()
	dcSessions.byToken["Bearer "+bearerA] = dcA
	dcSessions.Unlock()
	if status := call(http.MethodDelete, "/account-link", rtwA, "", linkBody(2), nil); status != 409 {
		t.Fatalf("stale disable revision status=%d", status)
	}
	var disabled linking.Link
	if status := call(http.MethodDelete, "/account-link", rtwA, "", linkBody(1), &disabled); status != 200 || disabled.State != "disabled" || disabled.Revision != 2 {
		t.Fatalf("disable status=%d link=%+v", status, disabled)
	}
	if status := call(http.MethodPost, "/product-sessions/exchange", "", bearerA, nil, nil); status != 404 {
		t.Fatalf("disabled association exchanged: %d", status)
	}
	if status := call(http.MethodPost, "/account-link", rtwB, bearerA, linkBody(0), nil); status != 409 {
		t.Fatalf("historical DC UUID taken by other UID: %d", status)
	}
	var rebound linking.Link
	if status := call(http.MethodPost, "/account-link", rtwA, bearerB, linkBody(2), &rebound); status != 200 || rebound.Revision != 3 || rebound.DCUserID != dcB {
		t.Fatalf("explicit rebind status=%d link=%+v", status, rebound)
	}
	if status := call(http.MethodPost, "/product-sessions/exchange", "", bearerA, nil, nil); status != 404 {
		t.Fatalf("old DC account exchanged after rebind: %d", status)
	}
	dcSessions.Lock()
	delete(dcSessions.byToken, "Bearer "+bearerB)
	dcSessions.Unlock()
	if status := call(http.MethodPost, "/product-sessions/exchange", "", bearerB, nil, nil); status != 401 {
		t.Fatalf("revoked DC bearer exchanged: %d", status)
	}
	dcSessions.Lock()
	dcSessions.byToken["Bearer "+bearerB] = dcB
	dcSessions.Unlock()
	users.mu.Lock()
	users.statuses[1001] = 1
	users.mu.Unlock()
	if status := call(http.MethodPost, "/product-sessions/exchange", "", bearerB, nil, nil); status != 403 {
		t.Fatalf("inactive RTW UID exchanged: %d", status)
	}
}
