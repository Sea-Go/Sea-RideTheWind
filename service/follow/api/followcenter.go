// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/observability"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/response"
	"github.com/Sea-Go/Sea-RideTheWind/service/follow/api/internal/config"
	"github.com/Sea-Go/Sea-RideTheWind/service/follow/api/internal/handler"
	"github.com/Sea-Go/Sea-RideTheWind/service/follow/api/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/follow/api/internal/svc"
	followcommon "github.com/Sea-Go/Sea-RideTheWind/service/follow/common"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpx"
)

var configFile = flag.String("f", "etc/followcenter.yaml", "the config file")

func main() {
	flag.Parse()

	metrics.InitMetrics()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	logx.MustSetup(c.Log)
	logger.Init(c.Name)

	httpTimeout := observability.DisableNativeRestTimeout(&c.RestConf)
	server := rest.MustNewServer(c.RestConf)
	server.Use(observability.NewHTTPMiddleware(c.Name, httpTimeout, observability.SlowThreshold()))
	defer server.Stop()

	httpx.SetOkHandler(func(ctx context.Context, v interface{}) interface{} {
		return &response.Response{
			Code: followcommon.Success,
			Msg:  followcommon.GetErrMsg(followcommon.Success),
			Data: v,
		}
	})

	httpx.SetErrorHandler(func(err error) (int, interface{}) {
		code := followcommon.ErrorServerCommon
		msg := followcommon.GetErrMsg(code)
		var codeErr *followcommon.CodeError
		if errors.As(err, &codeErr) {
			code = codeErr.Code
			msg = codeErr.Msg
		}
		return http.StatusOK, &response.Response{
			Code: code,
			Msg:  msg,
			Data: nil,
		}
	})

	ctx := svc.NewServiceContext(c)
	handler.RegisterHandlers(server, ctx)

	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}
