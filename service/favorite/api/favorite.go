package main

import (
	"flag"
	"fmt"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/observability"
	"sea-try-go/service/favorite/api/internal/config"
	"sea-try-go/service/favorite/api/internal/handler"
	"sea-try-go/service/favorite/api/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/favorite.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	logx.MustSetup(c.Log)
	logger.Init(c.Name)

	httpTimeout := observability.DisableNativeRestTimeout(&c.RestConf)
	server := rest.MustNewServer(c.RestConf)
	server.Use(observability.NewHTTPMiddleware(c.Name, httpTimeout, observability.SlowThreshold()))
	defer server.Stop()

	ctx := svc.NewServiceContext(c)
	handler.RegisterHandlers(server, ctx)

	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}
