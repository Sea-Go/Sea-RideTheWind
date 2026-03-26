package svc

import (
	"sea-try-go/service/follow/rpc/internal/config"
	"sea-try-go/service/follow/rpc/internal/model"
	"sea-try-go/service/user/user/rpc/userservice"

	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config      config.Config
	FollowModel *model.FollowModel
	UserRpc     userservice.UserService
}

func NewServiceContext(c config.Config) *ServiceContext {
	db := model.InitDB(model.DBConf{
		Host:     c.Postgres.Host,
		Port:     c.Postgres.Port,
		User:     c.Postgres.User,
		Password: c.Postgres.Password,
		DBName:   c.Postgres.DBName,
		Mode:     c.Postgres.Mode,
	})

	return &ServiceContext{
		Config:      c,
		FollowModel: model.NewFollowModel(db),
		UserRpc:     userservice.NewUserService(zrpc.MustNewClient(c.UserRpc)),
	}
}
