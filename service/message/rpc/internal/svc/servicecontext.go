package svc

import (
	"sea-try-go/service/common/snowflake"
	"sea-try-go/service/message/rpc/internal/config"
	"sea-try-go/service/message/rpc/internal/model"
	"sea-try-go/service/user/admin/rpc/adminservice"
	"sea-try-go/service/user/user/rpc/userservice"

	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config       config.Config
	MessageModel *model.MessageModel
	UserRpc      userservice.UserService
	AdminRpc     adminservice.AdminService
}

func NewServiceContext(c config.Config) *ServiceContext {
	snowflake.Init()
	db := model.InitDB(model.DBConf{
		Host:     c.Postgres.Host,
		Port:     c.Postgres.Port,
		User:     c.Postgres.User,
		Password: c.Postgres.Password,
		DBName:   c.Postgres.DBName,
		Mode:     c.Postgres.Mode,
	})

	return &ServiceContext{
		Config:       c,
		MessageModel: model.NewMessageModel(db),
		UserRpc:      userservice.NewUserService(zrpc.MustNewClient(c.UserRpc)),
		AdminRpc:     adminservice.NewAdminService(zrpc.MustNewClient(c.AdminRpc)),
	}
}
