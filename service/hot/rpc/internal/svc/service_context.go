package svc

import (
	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/internal/config"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-queue/kq"
)

type ServiceContext struct {
	Config      config.Config
	RedisClient *redis.Client
	KqPusher    *kq.Pusher
}

func NewServiceContext(c config.Config) *ServiceContext {
	rds := redis.NewClient(&redis.Options{
		Addr:     c.RedisConf.Host,
		Password: c.RedisConf.Pass,
	})

	pusher := kq.NewPusher(c.KqPusherConf.Brokers, c.KqPusherConf.Topic)

	return &ServiceContext{
		Config:      c,
		RedisClient: rds,
		KqPusher:    pusher,
	}
}
