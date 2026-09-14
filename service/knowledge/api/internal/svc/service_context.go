package svc

import (
	"context"
	"fmt"
	"time"

	"sea-try-go/service/knowledge/api/internal/config"
	"sea-try-go/service/knowledge/api/internal/middleware"
	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/telemetry"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/zeromicro/go-zero/rest"
)

type ServiceContext struct {
	Config        config.Config
	Administrator rest.Middleware
	Worker        rest.Middleware
	Store         *model.Store
}

func NewServiceContext(c config.Config, observer *telemetry.Runtime) (*ServiceContext, error) {
	if c.Auth.AccessSecret == "" || c.WorkerToken == "" || len(c.AdministratorIDs) == 0 {
		return nil, fmt.Errorf("auth secret, worker token and administrator identities required")
	}
	pc, err := pgxpool.ParseConfig(c.Postgres.DSN)
	if err != nil {
		return nil, fmt.Errorf("invalid postgres configuration")
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
	store := model.New(pool, objects, model.WithObservability(observer))
	if c.Postgres.Migrate {
		if err = store.Migrate(ctx); err != nil {
			return fail(err)
		}
	}
	return &ServiceContext{Config: c, Store: store, Administrator: middleware.NewAdministratorMiddleware(c.AdministratorIDs).Handle, Worker: middleware.NewWorkerMiddleware(c.WorkerToken).Handle}, nil
}
func (s *ServiceContext) Close() { s.Store.DB.Close() }
