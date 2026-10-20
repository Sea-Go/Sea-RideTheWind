package svc

import (
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/config"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/model"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

type ServiceContext struct {
	Config    config.Config
	UserModel *model.UserModel
	BizRedis  *redis.Redis
}

func NewServiceContext(c config.Config) *ServiceContext {
	dbConfig := model.DBConf{
		Host:     c.Postgres.Host,
		Port:     c.Postgres.Port,
		User:     c.Postgres.User,
		Password: c.Postgres.Password,
		DBName:   c.Postgres.DBName,
		Mode:     c.Postgres.Mode,
	}
	db := model.InitDB(dbConfig)
	return &ServiceContext{
		Config:    c,
		UserModel: model.NewUserModel(db),
		BizRedis:  redis.MustNewRedis(c.BizRedis),
	}
}
