// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package svc

import (
	"context"
	"fmt"
	"time"

	"sea-try-go/service/user/user/api/internal/config"
	"sea-try-go/service/user/user/api/internal/middleware"
	"sea-try-go/service/user/user/identity/linking"
	"sea-try-go/service/user/user/rpc/userservice"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config                   config.Config
	UserRpc                  userservice.UserService
	CheckBlacklistMiddleware rest.Middleware
	AccountLink              *linking.Service
	linkDB                   *pgxpool.Pool
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	redisDb := redis.MustNewRedis(c.BizRedis)
	service := &ServiceContext{
		Config:                   c,
		UserRpc:                  userservice.NewUserService(zrpc.MustNewClient(c.UserRpc)),
		CheckBlacklistMiddleware: middleware.NewCheckBlacklistMiddleware(redisDb).Handle,
	}
	if !c.AccountLink.Enabled {
		return service, nil
	}
	if c.AccountLink.PostgresDSN == "" || c.AccountLink.DataCenterMeURL == "" ||
		c.UserAuth.AccessSecret == "" {
		return nil, fmt.Errorf("account linking requires explicit RTW database, DC auth/me and user JWT configuration")
	}
	verifier, err := linking.NewDCVerifier(c.AccountLink.DataCenterMeURL, nil)
	if err != nil {
		return nil, err
	}
	pc, err := pgxpool.ParseConfig(c.AccountLink.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("invalid account-link PostgreSQL configuration")
	}
	pc.MaxConns = 8
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("account-link PostgreSQL unavailable: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("account-link PostgreSQL unavailable: %w", err)
	}
	service.linkDB = pool
	service.AccountLink = &linking.Service{Store: linking.NewStore(pool), DC: verifier,
		Users: service.UserRpc, JWTSecret: c.UserAuth.AccessSecret}
	return service, nil
}

func (s *ServiceContext) Close() {
	if s != nil && s.linkDB != nil {
		s.linkDB.Close()
	}
}
