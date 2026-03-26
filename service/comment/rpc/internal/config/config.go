package config

import (
	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	DB struct {
		DataSource string
	}
	KqPusherConf struct {
		Brokers []string
		Topic   string
	}
	BizRedis       redis.RedisConf
	KqConsumerConf kq.KqConf
	MessageRpc     zrpc.RpcClientConf
	ArticleRpc     zrpc.RpcClientConf
}
