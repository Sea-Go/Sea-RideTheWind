package config

import "github.com/zeromicro/go-zero/zrpc"

type Postgres struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	Mode     string
}

type Config struct {
	zrpc.RpcServerConf
	Postgres Postgres
	UserRpc  zrpc.RpcClientConf
	AdminRpc zrpc.RpcClientConf
	List     struct {
		DefaultLimit int32
		MaxLimit     int32
	}
	Broadcast struct {
		PageSize int64
	}
}
