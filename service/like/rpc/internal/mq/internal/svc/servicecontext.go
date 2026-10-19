package svc

import (
	"log"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/rpc/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/rpc/internal/mq/internal/config"

	"github.com/zeromicro/go-queue/kq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type ServiceContext struct {
	Config                config.Config
	LikeRecordModel       model.LikeRecordModel
	LikeConsumeInboxModel model.LikeConsumeInboxModel
	LikeOutboxEventModel  model.LikeOutboxEventModel
	HotEventPusher        *kq.Pusher
}

func NewServiceContext(c config.Config) *ServiceContext {
	db, err := gorm.Open(postgres.Open(c.DB.DataSource), &gorm.Config{})
	if err != nil {
		log.Fatalf("连接失败:%v", err)
	}
	return &ServiceContext{
		Config:                c,
		LikeRecordModel:       model.NewLikeRecordModel(db),
		LikeConsumeInboxModel: model.NewLikeConsumeInboxModel(db),
		LikeOutboxEventModel:  model.NewLikeOutboxEventModel(db),
		HotEventPusher:        kq.NewPusher(c.HotEventPusherConf.Brokers, c.HotEventPusherConf.Topic),
	}
}
