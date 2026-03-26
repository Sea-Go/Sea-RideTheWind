package config

import (
	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/zrpc"
)

type Postgres struct {
	Host     string
	Dbname   string
	Password string
	Port     string
	User     string
}

type Config struct {
	zrpc.RpcServerConf
	Postgres                      Postgres
	KqPusherConf                  kq.KqConf
	KqConsumerConf                kq.KqConf
	ArticleSyncPusherConf         kq.KqConf
	ArticleSyncResultConsumerConf kq.KqConf
	SecurityConf                  zrpc.RpcClientConf
	MessageRpc                    zrpc.RpcClientConf
	ArticleSyncOutbox             struct {
		PollIntervalMs int
		BatchSize      int
		MaxRetry       int
	}
	MinIO struct {
		Endpoint        string
		PublicBaseURL   string
		AccessKeyID     string
		SecretAccessKey string
		UseSSL          bool
		BucketName      string
		ImagePath       string
		ArticlePath     string
	}
	HotEventPusherConf struct {
		Brokers []string
		Topic   string
	}
}
