// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"sea-try-go/service/comment/api/internal/config"
	"sea-try-go/service/comment/api/internal/handler"
	"sea-try-go/service/comment/api/internal/svc"
	"sea-try-go/service/comment/common/errmsg"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/response"

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

	server := rest.MustNewServer(c.RestConf)
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
