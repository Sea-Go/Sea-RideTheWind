package main

import (
	"flag"
	"fmt"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/follow/rpc/internal/config"
	"sea-try-go/service/follow/rpc/internal/metrics"
	"sea-try-go/service/follow/rpc/internal/server"
	"sea-try-go/service/follow/rpc/internal/svc"
	"sea-try-go/service/follow/rpc/pb"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/follow.yaml", "the config file")

func main() {
	flag.Parse()

	metrics.InitMetrics()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	logx.MustSetup(c.Log)
	logger.Init(c.Name)
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterFollowServiceServer(grpcServer, server.NewFollowServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
