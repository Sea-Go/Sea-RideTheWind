package main

import (
	"flag"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/observability"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/config"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/server"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/user.yaml", "the config file")

func main() {
	metrics.InitMetrics()
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	logx.MustSetup(c.Log)
	logger.Init(c.Name)
	rpcTimeout := observability.DisableNativeRpcTimeout(&c.RpcServerConf)

	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterUserServiceServer(grpcServer, server.NewUserServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(observability.NewUnaryServerInterceptor(rpcTimeout, observability.SlowThreshold()))
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
