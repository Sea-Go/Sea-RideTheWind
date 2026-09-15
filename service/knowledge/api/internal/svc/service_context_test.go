package svc

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"

	"sea-try-go/service/knowledge/api/internal/config"
	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/user/user/rpc/pb"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
)

func TestProductIdentityRequiresUserIssuerAndRPC(t *testing.T) {
	var c config.Config
	c.Auth.AccessSecret = "admin-test-secret"
	c.WorkerToken = "worker-test-token"
	c.AdministratorIDs = []string{"1"}
	if _, err := NewServiceContext(c, nil); err == nil || !strings.Contains(err.Error(), "user auth") {
		t.Fatalf("missing user issuer accepted: %v", err)
	}
	c.UserAuth.AccessSecret = "user-test-secret"
	if _, err := NewServiceContext(c, nil); err == nil || !strings.Contains(err.Error(), "user RPC configuration") {
		t.Fatalf("missing user RPC accepted: %v", err)
	}
}

func TestContinuousSubjectRefV2CandidateCannotStartInProMode(t *testing.T) {
	var c config.Config
	c.Auth.AccessSecret = "admin-test-secret"
	c.UserAuth.AccessSecret = "user-test-secret"
	c.WorkerToken = "worker-test-token"
	c.AdministratorIDs = []string{"1"}
	c.UserRpc.Endpoints = []string{"127.0.0.1:12345"}
	c.Mode = "pro"
	c.SubjectRefV2Writes.Enabled = true
	if _, err := NewServiceContext(c, nil); err == nil ||
		!strings.Contains(err.Error(), "limited to a marked local test database") {
		t.Fatalf("production mode reached candidate database setup: %v", err)
	}
}

func TestWikiCompileJobCandidateStopsBeforeProductionOrRemoteEffects(t *testing.T) {
	var c config.Config
	c.Auth.AccessSecret = "admin-test-secret"
	c.UserAuth.AccessSecret = "user-test-secret"
	c.WorkerToken = "worker-test-token"
	c.AdministratorIDs = []string{"1"}
	c.UserRpc.Endpoints = []string{"127.0.0.1:12345"}
	c.WikiCompileJobs.Enabled = true
	c.WikiCompileJobs.Endpoint = "http://127.0.0.1:18181/v1/jobs"
	c.WikiCompileJobs.Token = "test-dc-job-service-token"
	c.WikiCompileJobs.IntervalMillis = 1000
	c.Mode = "pro"
	if _, err := NewServiceContext(c, nil); err == nil ||
		!strings.Contains(err.Error(), "candidate requires dev/test") {
		t.Fatalf("Wiki job switch reached a production database or DC: %v", err)
	}
	c.Mode = "dev"
	c.WikiCompileJobs.Endpoint = "https://jobs.example.test/v1/jobs"
	if _, err := NewServiceContext(c, nil); err == nil ||
		!strings.Contains(err.Error(), "explicit loopback") {
		t.Fatalf("Wiki job switch accepted a remote endpoint: %v", err)
	}
	c.WikiCompileJobs.Endpoint = "http://127.0.0.1:18181/v1/events"
	if _, err := NewServiceContext(c, nil); err == nil ||
		!strings.Contains(err.Error(), "explicit loopback") {
		t.Fatalf("Wiki job switch used H04 Eventing as a DC Job endpoint: %v", err)
	}
}

func TestV2SearchScopeCannotStartWithoutStageThreeOwnerGate(t *testing.T) {
	var c config.Config
	c.Auth.AccessSecret = "admin-test-secret"
	c.UserAuth.AccessSecret = "user-test-secret"
	c.WorkerToken = "worker-test-token"
	c.AdministratorIDs = []string{"1"}
	c.UserRpc.Endpoints = []string{"127.0.0.1:12345"}
	c.Mode = "dev"
	c.SearchSummary.ScopeVersion = "v2"
	if _, err := NewServiceContext(c, nil); err == nil ||
		!strings.Contains(err.Error(), "local SubjectRef v2 write gate") {
		t.Fatalf("v2 Summary scope reached DB or network without owner gate: %v", err)
	}
	c.SearchSummary.ScopeVersion = "v1"
	c.SearchTools.ScopeVersion = "v3"
	if _, err := NewServiceContext(c, nil); err == nil ||
		!strings.Contains(err.Error(), "must be v1 or v2") {
		t.Fatalf("unknown Tool scope version reached DB or network: %v", err)
	}
	c.SearchTools.ScopeVersion = "v2"
	c.SubjectRefV2Writes.Enabled = true
	c.Mode = "pro"
	if _, err := NewServiceContext(c, nil); err == nil ||
		!strings.Contains(err.Error(), "limited to a marked local test database") {
		t.Fatalf("formal pro mode bypassed Stage3 candidate gate: %v", err)
	}
}

func TestContinuousSubjectRefV2CandidateServiceAssemblyRequiresOwnerGate(t *testing.T) {
	dsn, nonce := os.Getenv("KNOWLEDGE_TEST_DSN"), os.Getenv("KNOWLEDGE_SUBJECTREF_V2_TEST_NONCE")
	if dsn == "" || nonce == "" {
		t.Skip("isolated PG16 runner provisions a DB-owner marker before candidate assembly")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := model.New(pool, objects)
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../scripts/migrate-subjectref-v2-storage.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(raw), pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	pb.RegisterUserServiceServer(server, pb.UnimplementedUserServiceServer{})
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	defer func() { server.Stop(); _ = listener.Close(); <-done }()
	var c config.Config
	c.Auth.AccessSecret = "admin-test-secret"
	c.UserAuth.AccessSecret = "user-test-secret"
	c.WorkerToken = "worker-test-token"
	c.AdministratorIDs = []string{"1"}
	c.UserRpc.Endpoints = []string{listener.Addr().String()}
	c.Mode = "dev"
	c.Postgres.DSN = dsn
	c.Postgres.MaxConnections = 8
	c.Objects.Backend = "local"
	c.Objects.LocalDirectory = t.TempDir()
	c.SubjectRefV2Writes.Enabled = true
	c.SubjectRefV2Writes.LocalTestNonce = strings.Repeat("0", 64)
	if assembled, err := NewServiceContext(c, nil); !errors.Is(err, model.ErrUnavailable) || assembled != nil {
		if assembled != nil {
			assembled.Close()
		}
		t.Fatalf("wrong DB-owner marker assembled v2 writes: %v", err)
	}
	c.SubjectRefV2Writes.LocalTestNonce = nonce
	assembled, err := NewServiceContext(c, nil)
	if err != nil {
		t.Fatalf("valid owner-marked DB rejected full service assembly: %v", err)
	}
	assembled.Close()
}
