package config

import "github.com/zeromicro/go-zero/rest"

type Config struct {
	Delivery struct {
		Enabled        bool   `json:",default=false"`
		Endpoint       string `json:",optional"`
		Token          string `json:",optional"`
		IntervalMillis int    `json:",default=1000"`
	}
	rest.RestConf
	Auth struct {
		AccessSecret string
		AccessExpire int64
	}
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
