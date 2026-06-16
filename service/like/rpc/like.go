package main

import (
	"flag"
	"fmt"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/observability"
	"sea-try-go/service/like/rpc/internal/config"
	"sea-try-go/service/like/rpc/internal/model"
	"sea-try-go/service/like/rpc/internal/server"
	"sea-try-go/service/like/rpc/internal/svc"
	"sea-try-go/service/like/rpc/pb"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/like.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	logger.Init(c.Name)
	rpcTimeout := observability.DisableNativeRpcTimeout(&c.RpcServerConf)
	model.InitDB(c.Storage.Postgres.DataSource)
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterLikeServiceServer(grpcServer, server.NewLikeServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(observability.NewUnaryServerInterceptor(rpcTimeout, observability.SlowThreshold()))
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
