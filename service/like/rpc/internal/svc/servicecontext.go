package svc

import (
	"sea-try-go/service/like/rpc/internal/config"
	"sea-try-go/service/like/rpc/internal/model"
	"sea-try-go/service/message/rpc/messageservice"

	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type ServiceContext struct {
	Config            config.Config
	BizRedis          *redis.Redis
	DB                *gorm.DB
	KafkaPusher       *kq.Pusher
	TaskUserPusher    *kq.Pusher
	TaskArticlePusher *kq.Pusher
	LikeModel         model.LikeRecordModel
	MessageRpc        messageservice.MessageService
}

func NewServiceContext(c config.Config) *ServiceContext {
	bizRedis := redis.MustNewRedis(c.Storage.Redis.RedisConf)
	dbConn, err := gorm.Open(postgres.Open(c.Storage.Postgres.DataSource), &gorm.Config{})
	if err != nil {
		panic("PostgreSQL连接失败" + err.Error())
	}
	sqlDB, err := dbConn.DB()
	if err == nil {
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(100)
	}
	kafkaPusher := kq.NewPusher(c.KafkaConf.Brokers, c.KafkaConf.Topic)
	return &ServiceContext{
		Config:            c,
		BizRedis:          bizRedis,
		DB:                dbConn,
		KafkaPusher:       kafkaPusher,
		TaskUserPusher:    newOptionalPusher(c.TaskUserPusherConf),
		TaskArticlePusher: newOptionalPusher(c.TaskArticlePusherConf),
		LikeModel:         model.NewLikeRecordModel(dbConn),
		MessageRpc:        messageservice.NewMessageService(zrpc.MustNewClient(c.MessageRpc)),
	}
}

func newOptionalPusher(conf kq.KqConf) *kq.Pusher {
	if len(conf.Brokers) == 0 || conf.Topic == "" {
		return nil
	}

	return kq.NewPusher(conf.Brokers, conf.Topic)
}
