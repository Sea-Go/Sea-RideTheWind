// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package svc

import (
	"github.com/Sea-Go/Sea-RideTheWind/service/like/api/internal/config"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/rpc/likeservice"

	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config  config.Config
	LikeRpc likeservice.LikeService
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:  c,
		LikeRpc: likeservice.NewLikeService(zrpc.MustNewClient(c.LikeRpc)),
	}
}
