// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package svc

import (
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/config"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/middleware"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/adminservice"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config                   config.Config
	AdminRpc                 adminservice.AdminService
	CheckBlacklistMiddleware rest.Middleware
}

func NewServiceContext(c config.Config) *ServiceContext {

	redisDb := redis.MustNewRedis(c.BizRedis)
	return &ServiceContext{
		Config:                   c,
		AdminRpc:                 adminservice.NewAdminService(zrpc.MustNewClient(c.AdminRpc)),
		CheckBlacklistMiddleware: middleware.NewCheckBlacklistMiddleware(redisDb).Handle,
	}
}
