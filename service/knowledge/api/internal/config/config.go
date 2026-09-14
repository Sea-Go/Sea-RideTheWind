package config

import (
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
	"sea-try-go/service/knowledge/api/internal/telemetry"
)

type Config struct {
	Delivery struct {
		Enabled        bool   `json:",default=false"`
		Endpoint       string `json:",optional"`
		Token          string `json:",optional"`
		IntervalMillis int    `json:",default=1000"`
	}
	rest.RestConf
	Observability telemetry.Config
	Auth          struct {
		AccessSecret string
		AccessExpire int64
	}
	UserAuth struct {
		AccessSecret string
	}
	UserRpc          zrpc.RpcClientConf
	AdministratorIDs []string
	WorkerToken      string
	Postgres         struct {
		DSN            string
		MaxConnections int32 `json:",default=8"`
		Migrate        bool  `json:",default=false"`
	}
	Objects struct {
		Backend        string
		LocalDirectory string `json:",optional"`
		Endpoint       string `json:",optional"`
		Bucket         string `json:",optional"`
		AccessKey      string `json:",optional"`
		SecretKey      string `json:",optional"`
		Secure         bool   `json:",default=true"`
	}
}
