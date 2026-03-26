// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package svc

import (
	"sea-try-go/service/follow/api/internal/config"
	"sea-try-go/service/follow/api/internal/middleware"
	"sea-try-go/service/follow/rpc/followservice"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config                   config.Config
	FollowRpc                followservice.FollowService
	CheckBlacklistMiddleware rest.Middleware
}

func NewServiceContext(c config.Config) *ServiceContext {
	redisDB := redis.MustNewRedis(c.BizRedis)

	return &ServiceContext{
		Config:                   c,
		FollowRpc:                followservice.NewFollowService(zrpc.MustNewClient(c.FollowRpc)),
		CheckBlacklistMiddleware: middleware.NewCheckBlacklistMiddleware(redisDB).Handle,
	}
}
