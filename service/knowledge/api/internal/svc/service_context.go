package svc

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"sea-try-go/service/knowledge/api/internal/config"
	"sea-try-go/service/knowledge/api/internal/middleware"
	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/telemetry"
	"sea-try-go/service/user/user/identity"
	"sea-try-go/service/user/user/rpc/userservice"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config        config.Config
	Administrator rest.Middleware
	Worker        rest.Middleware
	Store         *model.Store
	UserRpc       identity.UserReader
	SearchHTTP    *http.Client
	userRpcClient zrpc.Client
}

func NewServiceContext(c config.Config, observer *telemetry.Runtime) (*ServiceContext, error) {
	if c.Auth.AccessSecret == "" || c.UserAuth.AccessSecret == "" || c.WorkerToken == "" || len(c.AdministratorIDs) == 0 {
		return nil, fmt.Errorf("administrator auth, user auth, worker token and administrator identities required")
	}
	if _, err := c.UserRpc.BuildTarget(); err != nil {
		return nil, fmt.Errorf("user RPC configuration: %w", err)
	}
	if c.SubjectRefV2Writes.Enabled && c.Mode == "pro" {
		return nil, fmt.Errorf("SubjectRef v2 continuous-write candidate is limited to a marked local test database")
	}
	if c.WikiCompileJobs.Enabled {
		u, err := url.Parse(c.WikiCompileJobs.Endpoint)
		if err != nil || u == nil || u.Scheme != "http" || u.User != nil ||
			u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/jobs" ||
			net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() ||
			c.WikiCompileJobs.Token == "" || c.WikiCompileJobs.IntervalMillis < 100 ||
			(c.Mode != "dev" && c.Mode != "test") {
			return nil, fmt.Errorf("Wiki Compile DC Jobs candidate requires dev/test and an explicit loopback /v1/jobs service endpoint")
		}
	}
	for _, version := range []string{c.SearchSummary.ScopeVersion, c.SearchTools.ScopeVersion} {
		if version != "" && version != "v1" && version != "v2" {
			return nil, fmt.Errorf("search scope version must be v1 or v2")
		}
		if version == "v2" && !c.SubjectRefV2Writes.Enabled {
			return nil, fmt.Errorf("v2 search scope requires the local SubjectRef v2 write gate")
		}
	}
	if c.SearchSummary.Endpoint != "" || c.SearchSummary.ScopeKey != "" {
		u, err := url.Parse(c.SearchSummary.Endpoint)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") ||
			u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/search/summary" ||
			len(c.SearchSummary.ScopeKey) < 32 {
			return nil, fmt.Errorf("search summary requires a private /v1/search/summary endpoint and scope key of at least 32 bytes")
		}
		if c.SearchSummary.FastTimeoutMillis < 1000 || c.SearchSummary.FastTimeoutMillis > 60000 ||
			c.SearchSummary.DetailedTimeoutMillis < c.SearchSummary.FastTimeoutMillis ||
			c.SearchSummary.DetailedTimeoutMillis > 180000 ||
			c.Timeout < int64(c.SearchSummary.DetailedTimeoutMillis+5000) {
			return nil, fmt.Errorf("search summary requires bounded fast/detailed budgets and a larger RTW HTTP timeout")
		}
	}
	if c.SearchTools.Endpoint != "" || c.SearchTools.ScopeKey != "" {
		u, err := url.Parse(c.SearchTools.Endpoint)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") ||
			u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/search/tools/search" ||
			len(c.SearchTools.ScopeKey) < 32 {
			return nil, fmt.Errorf("search Tools require a private /v1/search/tools/search endpoint and scope key of at least 32 bytes")
		}
		if c.SearchTools.FastTimeoutMillis < 1000 || c.SearchTools.FastTimeoutMillis > 60000 ||
			c.SearchTools.DetailedTimeoutMillis < c.SearchTools.FastTimeoutMillis ||
			c.SearchTools.DetailedTimeoutMillis > 180000 ||
			c.Timeout < int64(c.SearchTools.DetailedTimeoutMillis+5000) {
			return nil, fmt.Errorf("search Tools require bounded fast/detailed budgets and a larger RTW HTTP timeout")
		}
	}
	pc, err := pgxpool.ParseConfig(c.Postgres.DSN)
	if err != nil {
		return nil, fmt.Errorf("invalid postgres configuration")
	}
	if c.WikiCompileJobs.Enabled {
		ip := net.ParseIP(pc.ConnConfig.Host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("Wiki Compile Jobs candidate requires a task-owned loopback PostgreSQL")
		}
	}
	if c.Postgres.MaxConnections < 2 {
		return nil, fmt.Errorf("postgres pool requires at least two connections")
	}
	pc.MaxConns = c.Postgres.MaxConnections
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	fail := func(err error) (*ServiceContext, error) { pool.Close(); return nil, err }
	if err = pool.Ping(ctx); err != nil {
		return fail(fmt.Errorf("postgres unavailable"))
	}
	var objects object.Store
	switch c.Objects.Backend {
	case "local":
		if c.Mode == "pro" {
			return fail(fmt.Errorf("local object backend is a development adapter"))
		}
		objects, err = object.NewLocal(c.Objects.LocalDirectory)
	case "s3":
		if c.Objects.Endpoint == "" || c.Objects.Bucket == "" {
			return fail(fmt.Errorf("s3 endpoint and bucket required"))
		}
		var client *minio.Client
		client, err = minio.New(c.Objects.Endpoint, &minio.Options{Creds: credentials.NewStaticV4(c.Objects.AccessKey, c.Objects.SecretKey, ""), Secure: c.Objects.Secure})
		if err == nil {
			var exists bool
			exists, err = client.BucketExists(ctx, c.Objects.Bucket)
			if err == nil && !exists {
				err = fmt.Errorf("configured s3 bucket does not exist")
			}
		}
		if err == nil {
			objects = object.NewS3(client, c.Objects.Bucket)
		}
	default:
		err = fmt.Errorf("object backend must be local or s3")
	}
	if err != nil {
		return fail(err)
	}
	storeOptions := []model.Option{model.WithObservability(observer)}
	if c.SubjectRefV2Writes.Enabled {
		storeOptions = append(storeOptions, model.WithContinuousSubjectRefV2Writes())
	}
	if c.WikiCompileJobs.Enabled {
		storeOptions = append(storeOptions, model.WithWikiCompileJobs())
	}
	store := model.New(pool, objects, storeOptions...)
	if c.Postgres.Migrate {
		if err = store.Migrate(ctx); err != nil {
			return fail(err)
		}
	}
	if err = store.DetectSearchScopeVersions(ctx); err != nil {
		return fail(err)
	}
	if c.SubjectRefV2Writes.Enabled {
		if err = store.CheckContinuousSubjectRefV2Writes(ctx, c.SubjectRefV2Writes.LocalTestNonce); err != nil {
			return fail(err)
		}
	}
	if c.WikiCompileJobs.Enabled {
		if err = store.CheckWikiCompileJobCandidate(ctx); err != nil {
			return fail(err)
		}
	}
	if c.SearchSummary.ScopeVersion == "v2" || c.SearchTools.ScopeVersion == "v2" {
		if err = store.RequireSearchScopeVersions(); err != nil {
			return fail(err)
		}
	}
	if c.SearchJudgments.Enabled {
		if err = store.CheckSearchJudgmentSchema(ctx); err != nil {
			return fail(err)
		}
	}
	if c.GroundingReviews.Enabled {
		if err = store.CheckGroundingReviewSchema(ctx); err != nil {
			return fail(err)
		}
	}
	if c.SearchSummary.Endpoint != "" {
		if err = store.CheckProductSearchSchema(ctx); err != nil {
			return fail(err)
		}
	}
	if c.SearchTools.Endpoint != "" {
		if err = store.CheckToolSearchSchema(ctx); err != nil {
			return fail(err)
		}
	}
	userClient, err := zrpc.NewClient(c.UserRpc)
	if err != nil {
		return fail(fmt.Errorf("user RPC configuration: %w", err))
	}
	return &ServiceContext{Config: c, Store: store, UserRpc: userservice.NewUserService(userClient), userRpcClient: userClient,
		SearchHTTP: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		Administrator: middleware.NewAdministratorMiddleware(c.AdministratorIDs).Handle,
		Worker:        middleware.NewWorkerMiddleware(c.WorkerToken).Handle}, nil
}
func (s *ServiceContext) Close() {
	if s.userRpcClient != nil {
		_ = s.userRpcClient.Conn().Close()
	}
	s.Store.DB.Close()
}
