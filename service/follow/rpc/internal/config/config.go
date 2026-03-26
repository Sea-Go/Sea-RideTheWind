package config

import "github.com/zeromicro/go-zero/zrpc"

type PostgresConf struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	Mode     string
}

type ListConf struct {
	DefaultLimit int32 `json:",default=20"`
	MaxLimit     int32 `json:",default=100"`
}

type RecommendConf struct {
	DefaultLimit int32 `json:",default=20"`
	MaxLimit     int32 `json:",default=50"`
	MaxDepth     int   `json:",default=3"`
	MaxFanout    int   `json:",default=50"`
}

type Config struct {
	zrpc.RpcServerConf
	Postgres  PostgresConf
	UserRpc   zrpc.RpcClientConf
	List      ListConf
	Recommend RecommendConf
}
