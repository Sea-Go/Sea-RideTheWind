package config

import (
	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"
)

type StorageConf struct {
	RocksDB  RocksDBConf
	Redis    RedisExtConf
	Postgres PostgresConf
}

type RocksDBConf struct {
	Path                     string `json:",default=./data/like_rocksdb"`
	WriterBufferSize         int    `json:",default=67108864"`
	MaxWriteBufferNumber     int    `json:",default=4"`
	TargetFileSizeBase       int    `json:",default=67108864"`
	MaxBackgroundCompactions int    `json:",default=4"`
}

type RedisExtConf struct {
	redis.RedisConf
	CacheTTL   int `json:",default=3600"`
	HotspotTTL int `json:",default=1800"`
	BatchSize  int `json:",default=1000"`
}

type PostgresConf struct {
	DataSource    string
	SyncBatchSize int `json:",default=10000"`
	SyncInterval  int `json:",default=300"`
}

type KafkaConf struct {
	Brokers []string
	Topic   string `json:",default=like_promotion_topic"`
}

type HotSpotConf struct {
	Detector      DetectorConf
	AntiAvalanche AntiAvalancheConf
}

type DetectorConf struct {
	Threshold  int `json:",default=100"`
	WindowSize int `json:",default=60"`
}

type AntiAvalancheConf struct {
	MaxConcurrent int `json:",default=1000"`
	QueueSize     int `json:",default=10000"`
	Timeout       int `json:",default=1000"`
}

type AntiBrushConf struct {
	RateLimit RateLimitConf
	UserLimit UserLimitConf
}

type RateLimitConf struct {
	Window      int `json:",default=60"`
	MaxRequests int `json:",default=60"`
}

type UserLimitConf struct {
	DailyMax     int `json:",default=1000"`
	PerMinuteMax int `json:",default=60"`
}

type Config struct {
	zrpc.RpcServerConf
	Storage     StorageConf
	KafkaConf   KafkaConf
	HotSpot     HotSpotConf
	AntiBrush   AntiBrushConf
	MessageRpc  zrpc.RpcClientConf
	KafkaPusher struct {
		Brokers []string
		Topic   string
	}
	TaskUserPusherConf    kq.KqConf
	TaskArticlePusherConf kq.KqConf
}
