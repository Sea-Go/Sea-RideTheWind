// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package svc

import (
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/config"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/middleware"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/userservice"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config                   config.Config
	UserRpc                  userservice.UserService
	CheckBlacklistMiddleware rest.Middleware
}

func NewServiceContext(c config.Config) *ServiceContext {
	redisDb := redis.MustNewRedis(c.BizRedis)
	return &ServiceContext{
		Config:                   c,
		UserRpc:                  userservice.NewUserService(zrpc.MustNewClient(c.UserRpc)),
		CheckBlacklistMiddleware: middleware.NewCheckBlacklistMiddleware(redisDb).Handle,
	}
}
