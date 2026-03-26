// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"strconv"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/response"
	"sea-try-go/service/message/api/internal/config"
	"sea-try-go/service/message/api/internal/handler"
	"sea-try-go/service/message/api/internal/metrics"
	"sea-try-go/service/message/api/internal/svc"
	messagecommon "sea-try-go/service/message/common"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpx"
)

var configFile = flag.String("f", "etc/messagecenter.yaml", "the config file")

func main() {
	flag.Parse()

	metrics.InitMetrics()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	logx.MustSetup(c.Log)
	logger.Init(c.Name)

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	httpx.SetOkHandler(func(ctx context.Context, v interface{}) interface{} {
		return &response.Response{
			Code: messagecommon.Success,
			Msg:  messagecommon.GetErrMsg(messagecommon.Success),
			Data: v,
		}
	})

	httpx.SetErrorHandler(func(err error) (int, interface{}) {
		code := messagecommon.ErrorServerCommon
		msg := messagecommon.GetErrMsg(code)
		var codeErr *messagecommon.CodeError
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

	logger.LogInfo(context.Background(), "Starting server at "+c.Host+":"+strconv.Itoa(c.Port))
	server.Start()
}
