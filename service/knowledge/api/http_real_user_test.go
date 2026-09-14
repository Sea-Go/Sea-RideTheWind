package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/user/user/identity/linking"
	"sea-try-go/service/user/user/rpc/pb"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type realUserServices struct {
	apiURL  string
	rpc     pb.UserServiceClient
	conn    *grpc.ClientConn
	dbDSN   string
	rpcStop func()
}

const (
	realUserDCOwnerBearer = "wh_access_rtw_h01_owner"
	realUserDCOtherBearer = "wh_access_rtw_h01_other"
)

func TestRealHTTPKnowledgeWorkflowWithUserCenter(t *testing.T) {
	if os.Getenv("KNOWLEDGE_REAL_USER_RPC_BINARY") == "" || os.Getenv("KNOWLEDGE_REAL_USER_API_BINARY") == "" {
		t.Skip("set KNOWLEDGE_REAL_USER_GATE=1 in the isolated acceptance script")
	}
	runRealHTTPKnowledgeWorkflow(t, true)
}

func startRealUserServices(t *testing.T, store *model.Store, secret string) *realUserServices {
	t.Helper()
	base, err := url.Parse(os.Getenv("KNOWLEDGE_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	name := "rtw_user_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err = store.DB.Exec(context.Background(), "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := store.DB.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	base.Path = "/" + name
	base.RawQuery = "sslmode=disable"
	root := t.TempDir()
	rpcPort := freeUserTestPort(t)
	apiPort := freeUserTestPort(t)
	rpcEndpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(rpcPort))
	userName := ""
	// The legacy User RPC builds a keyword DSN with an unquoted password=.
	// An empty value swallows the following dbname token in pgx parsing; the
	// isolated PostgreSQL cluster uses trust auth, so this is a test-only value.
	password := "test-only-unused-password"
	if base.User != nil {
		userName = base.User.Username()
		if value, ok := base.User.Password(); ok && value != "" {
			password = value
		}
	}
	rpcConfig := map[string]any{
		"Name": "real-user-rpc-test", "ListenOn": rpcEndpoint, "Mode": "test",
		"Log": map[string]any{"Mode": "console", "Level": "error"},
		"Postgres": map[string]any{"Host": base.Hostname(), "Port": base.Port(), "User": userName,
			"Password": password, "DBName": name, "Mode": "disable"},
		"BizRedis": map[string]any{"Host": "127.0.0.1:1", "Type": "node", "NonBlock": true},
		"UserAuth": map[string]any{"AccessSecret": secret, "AccessExpire": 3600},
	}
	rpcStop := startRealUserProcess(t, os.Getenv("KNOWLEDGE_REAL_USER_RPC_BINARY"), root, "user-rpc", rpcConfig)
	conn, err := grpc.NewClient(rpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	userRPC := pb.NewUserServiceClient(conn)
	deadline := time.Now().Add(15 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, err = userRPC.GetUser(ctx, &pb.GetUserReq{Uid: 1})
		cancel()
		if status.Code(err) == codes.NotFound {
			break // A real not-found response proves the handler and users table are ready.
		}
		if time.Now().After(deadline) {
			t.Fatalf("real User RPC did not serve GetUser: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	linkDB, err := pgxpool.New(context.Background(), base.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := linkDB.Exec(context.Background(), linking.MigrationSQL); err != nil {
		linkDB.Close()
		t.Fatal(err)
	}
	linkDB.Close()
	dcOwnerID, dcOtherID := uuid.NewString(), uuid.NewString()
	dcAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/auth/me" {
			http.NotFound(w, r)
			return
		}
		var id string
		switch r.Header.Get("Authorization") {
		case "Bearer " + realUserDCOwnerBearer:
			id = dcOwnerID
		case "Bearer " + realUserDCOtherBearer:
			id = dcOtherID
		default:
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
	}))
	t.Cleanup(dcAuth.Close)
	apiConfig := map[string]any{
		"Name": "real-usercenter-test", "Host": "127.0.0.1", "Port": apiPort, "Mode": "test",
		"Log":      map[string]any{"Mode": "console", "Level": "error"},
		"UserAuth": map[string]any{"AccessSecret": secret, "AccessExpire": 3600},
		"UserRpc":  map[string]any{"Endpoints": []string{rpcEndpoint}},
		"BizRedis": map[string]any{"Host": "127.0.0.1:1", "Type": "node", "NonBlock": true},
		"AccountLink": map[string]any{"Enabled": true, "PostgresDSN": base.String(),
			"DataCenterMeURL": dcAuth.URL + "/v1/auth/me"},
	}
	startRealUserProcess(t, os.Getenv("KNOWLEDGE_REAL_USER_API_BINARY"), root, "usercenter", apiConfig)
	apiURL := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort))
	deadline = time.Now().Add(15 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/usercenter/v1/unknown", nil)
		res, requestErr := http.DefaultClient.Do(req)
		cancel()
		if requestErr == nil {
			_ = res.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("real User Center API did not start: %v", requestErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return &realUserServices{apiURL: apiURL, rpc: userRPC, conn: conn, dbDSN: base.String(), rpcStop: rpcStop}
}

func freeUserTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func startRealUserProcess(t *testing.T, binary, root, name string, config any) func() {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, name+".json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, name+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-f", path)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-exited:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-exited
			}
			_ = logFile.Close()
			if t.Failed() {
				content, _ := os.ReadFile(logPath)
				t.Logf("%s process output:\n%s", name, content)
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func (s *realUserServices) registerAndLogin(t *testing.T, username string) (int64, string) {
	t.Helper()
	password := "test-only-password-123"
	request := func(path string, body any, out any) {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.Post(s.apiURL+path, "application/json", bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if err = json.Unmarshal(raw, &response); err != nil || res.StatusCode != 200 || response.Code != 200 {
			t.Fatalf("real User Center %s status=%d body=%s err=%v", path, res.StatusCode, raw, err)
		}
		if err = json.Unmarshal(response.Data, out); err != nil {
			t.Fatal(err)
		}
	}
	var registered struct {
		UID string `json:"uid"`
	}
	request("/usercenter/v1/user/register", map[string]string{
		"username": username, "password": password, "email": username + "@example.test",
	}, &registered)
	uid, err := strconv.ParseInt(registered.UID, 10, 64)
	if err != nil || uid <= 0 {
		t.Fatalf("real User Center registration UID=%q err=%v", registered.UID, err)
	}
	var loggedIn struct {
		Token string `json:"token"`
	}
	request("/usercenter/v1/user/login", map[string]string{"username": username, "password": password}, &loggedIn)
	if loggedIn.Token == "" {
		t.Fatal("real User Center did not sign a JWT")
	}
	return uid, loggedIn.Token
}

func (s *realUserServices) bindAndExchange(t *testing.T, uid int64, rtwToken, dcBearer string) string {
	t.Helper()
	bindBody := bytes.NewBufferString(`{"expected_revision":0}`)
	bind, err := http.NewRequest(http.MethodPost, s.apiURL+"/usercenter/v1/account-link", bindBody)
	if err != nil {
		t.Fatal(err)
	}
	bind.Header.Set("X-RTW-Authorization", "Bearer "+rtwToken)
	bind.Header.Set("Authorization", "Bearer "+dcBearer)
	bind.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(bind)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var link linking.Link
	if err := json.NewDecoder(response.Body).Decode(&link); err != nil || response.StatusCode != http.StatusOK ||
		link.UID != uid || link.State != "active" || link.Revision != 1 {
		t.Fatalf("real User Center H01 bind status=%d link=%+v err=%v", response.StatusCode, link, err)
	}
	exchange, err := http.NewRequest(http.MethodPost, s.apiURL+"/usercenter/v1/product-sessions/exchange", nil)
	if err != nil {
		t.Fatal(err)
	}
	exchange.Header.Set("Authorization", "Bearer "+dcBearer)
	tokenResponse, err := http.DefaultClient.Do(exchange)
	if err != nil {
		t.Fatal(err)
	}
	defer tokenResponse.Body.Close()
	var session linking.ProductSession
	if err := json.NewDecoder(tokenResponse.Body).Decode(&session); err != nil ||
		tokenResponse.StatusCode != http.StatusOK || session.Token == "" ||
		session.LinkRevision != 1 || session.ExpiresAtUnix-time.Now().Unix() > linking.ProductTTLSeconds ||
		session.ExpiresAtUnix <= time.Now().Unix() {
		t.Fatalf("real User Center H01 exchange status=%d revision=%d err=%v",
			tokenResponse.StatusCode, session.LinkRevision, err)
	}
	return session.Token
}

func (s *realUserServices) delete(t *testing.T, uid int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := s.rpc.DeleteUser(ctx, &pb.DeleteUserReq{Uid: uid})
	if err != nil || resp == nil || !resp.Success {
		t.Fatalf("real User RPC deletion failed: resp=%+v err=%v", resp, err)
	}
}

func (s *realUserServices) stopRPC(t *testing.T) {
	t.Helper()
	s.rpcStop()
}

func (s *realUserServices) markDisabled(t *testing.T, uid int64) {
	t.Helper()
	db, err := pgxpool.New(context.Background(), s.dbDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	result, err := db.Exec(context.Background(), "UPDATE users SET status=1 WHERE uid=$1", uid)
	if err != nil || result.RowsAffected() != 1 {
		t.Fatalf("disable user rows=%d err=%v", result.RowsAffected(), err)
	}
	data, err := json.Marshal(map[string]string{
		"username": "knowledge-history-owner", "password": "test-only-password-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(s.apiURL+"/usercenter/v1/user/login", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var response struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil || res.StatusCode != 200 || response.Code != 1011 {
		t.Fatalf("disabled User Center login status=%d code=%d err=%v", res.StatusCode, response.Code, err)
	}
}
