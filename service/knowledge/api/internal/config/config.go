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
	WikiCompileJobs struct {
		Enabled        bool   `json:",default=false"`
		Endpoint       string `json:",optional"` // exact loopback /v1/jobs URL
		Token          string `json:",optional"` // DC job service token, never a model bearer
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
	UserRpc       zrpc.RpcClientConf
	SearchSummary struct {
		Endpoint               string `json:",optional"`
		ScopeKey               string `json:",optional"`
		ScopeVersion           string `json:",default=v1"`
		FastTimeoutMillis      int    `json:",default=30000"`
		DetailedTimeoutMillis  int    `json:",default=90000"`
		AllowPartial           bool   `json:",default=false"`
		AllowLowerIntelligence bool   `json:",default=false"`
	}
	SearchTools struct {
		Endpoint               string `json:",optional"`
		ScopeKey               string `json:",optional"`
		ScopeVersion           string `json:",default=v1"`
		FastTimeoutMillis      int    `json:",default=30000"`
		DetailedTimeoutMillis  int    `json:",default=90000"`
		AllowPartial           bool   `json:",default=false"`
		AllowLowerIntelligence bool   `json:",default=false"`
	}
	SearchJudgments struct {
		Enabled bool `json:",default=false"`
	}
	WikiQualityJudgments struct {
		Enabled bool `json:",default=false"`
	}
	GroundingReviews struct {
		Enabled bool `json:",default=false"`
	}
	SubjectRefV2Writes struct {
		Enabled        bool   `json:",default=false"`
		LocalTestNonce string `json:",optional"`
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
