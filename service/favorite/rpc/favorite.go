package main

import (
	"flag"
	"fmt"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/observability"
	"sea-try-go/service/favorite/rpc/internal/config"
	"sea-try-go/service/favorite/rpc/internal/metrics"
	"sea-try-go/service/favorite/rpc/internal/server"
	"sea-try-go/service/favorite/rpc/internal/svc"
	favoritepb "sea-try-go/service/favorite/rpc/pb"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/favorite.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	logx.MustSetup(c.Log)
	ctx := svc.NewServiceContext(c)
	logger.Init(c.Name)
	metrics.InitMetrics(&c)
	rpcTimeout := observability.DisableNativeRpcTimeout(&c.RpcServerConf)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		favoritepb.RegisterFavoriteServiceServer(grpcServer, server.NewFavoriteServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(observability.NewUnaryServerInterceptor(rpcTimeout, observability.SlowThreshold()))
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
