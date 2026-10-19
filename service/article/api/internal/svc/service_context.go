// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package svc

import (
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/Sea-Go/Sea-RideTheWind/service/article/api/internal/config"
	"github.com/Sea-Go/Sea-RideTheWind/service/article/rpc/articleservice"
	"github.com/Sea-Go/Sea-RideTheWind/service/security/rpc/client/imagesecurityservice"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/userservice"
)

type ServiceContext struct {
	Config      config.Config
	ArticleRpc  articleservice.ArticleService
	SecurityRpc imagesecurityservice.ImageSecurityService
	UserRpc     userservice.UserService
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:      c,
		ArticleRpc:  articleservice.NewArticleService(zrpc.MustNewClient(c.ArticleRpcConf)),
		SecurityRpc: imagesecurityservice.NewImageSecurityService(zrpc.MustNewClient(c.SecurityRpcConf)),
		UserRpc:     userservice.NewUserService(zrpc.MustNewClient(c.UserRpcConf)),
	}
}
