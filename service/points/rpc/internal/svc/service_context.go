package svc

import (
	"github.com/Sea-Go/Sea-RideTheWind/service/points/rpc/internal/config"
	"github.com/Sea-Go/Sea-RideTheWind/service/points/rpc/internal/model"

	"github.com/zeromicro/go-queue/dq"
	"github.com/zeromicro/go-queue/kq"
)

type ServiceContext struct {
	Config              config.Config
	PointsModel         *model.PointsModel
	RetryDqPusherClient dq.Producer
	RetryDqConsumer     dq.Consumer
	DqPusherClient      dq.Producer
	DqConsumer          dq.Consumer
	KqPusherClient      *kq.Pusher
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
		Config:              c,
		PointsModel:         model.NewPointsModel(db),
		RetryDqPusherClient: dq.NewProducer(c.RetryDqConf.Beanstalks),
		RetryDqConsumer:     dq.NewConsumer(c.RetryDqConf),
		DqPusherClient:      dq.NewProducer(c.DqConf.Beanstalks),
		DqConsumer:          dq.NewConsumer(c.DqConf),
		KqPusherClient:      kq.NewPusher(c.KqPusherConf.Brokers, c.KqPusherConf.Topic),
	}
}
