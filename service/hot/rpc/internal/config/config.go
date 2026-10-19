package config

import (
	"github.com/Sea-Go/Sea-RideTheWind/service/hot/heavykeeper"

	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf

	RedisConf struct {
		Host string
		Type string `json:",default=node,options=node|cluster"`
		Pass string `json:",optional"`
	}

	KqPusherConf struct {
		Brokers []string
		Topic   string
	}
	KqConsumerConf kq.KqConf

	HeavyKeeper heavykeeper.Config
	Interaction InteractionConfig
}

type InteractionConfig struct {
	SyncEvery      int64         `json:",default=100"`
	TTL            int64         `json:",default=3600"`
	PartitionCount int           `json:",default=3"`
	Weights        []IndexWeight `json:",optional"`
}

type IndexWeight struct {
	Code   int    `json:"Code"`
	Name   string `json:"Name"`
	Weight int32  `json:"Weight"`
}
