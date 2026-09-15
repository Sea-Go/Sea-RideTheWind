package config

import "github.com/zeromicro/go-zero/zrpc"

type Config struct {
	zrpc.RpcServerConf
	Postgres struct {
		Host     string
		Port     string
		User     string
		Password string
		DBName   string
		Mode     string
	}
	UserRpc    zrpc.RpcClientConf
	ArticleRpc zrpc.RpcClientConf
	// SubjectRefV2Facts is an explicit local candidate switch. Omitted is v1.
	SubjectRefV2Facts bool `json:",optional"`
}
