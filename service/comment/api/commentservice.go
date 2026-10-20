// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/api/internal/config"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/api/internal/handler"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/observability"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/response"
	"net/http"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpx"
)

var configFile = flag.String("f", "etc/commentservice.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	logger.Init(c.Name)

	httpTimeout := observability.DisableNativeRestTimeout(&c.RestConf)
	server := rest.MustNewServer(c.RestConf)
	server.Use(observability.NewHTTPMiddleware(c.Name, httpTimeout, observability.SlowThreshold()))
	defer server.Stop()

	httpx.SetOkHandler(func(ctx context.Context, v interface{}) interface{} {
		return &response.Response{
			Code: errmsg.Success,
			Msg:  errmsg.GetErrMsg(errmsg.Success),
			Data: v,
		}
	})

	httpx.SetErrorHandler(func(err error) (int, interface{}) {
		code := errmsg.ErrorServerCommon
		msg := errmsg.GetErrMsg(code)
		var e *errmsg.CodeError
		if errors.As(err, &e) {
			code = e.Code
			msg = e.Msg
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
