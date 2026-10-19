package main

import (
	"context"
	"flag"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/observability"
	"github.com/Sea-Go/Sea-RideTheWind/service/security/rpc/internal/config"
	contentsecurityserviceServer "github.com/Sea-Go/Sea-RideTheWind/service/security/rpc/internal/server/contentsecurityservice"
	imagesecurityserviceServer "github.com/Sea-Go/Sea-RideTheWind/service/security/rpc/internal/server/imagesecurityservice"
	"github.com/Sea-Go/Sea-RideTheWind/service/security/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/security/rpc/pb"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
)

var configFile = flag.String("f", "etc/security.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	rpcTimeout := observability.DisableNativeRpcTimeout(&c.RpcServerConf)
	ctx := svc.NewServiceContext(c)

	logger.Init("security-rpc")

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterContentSecurityServiceServer(grpcServer, contentsecurityserviceServer.NewContentSecurityServiceServer(ctx))
		pb.RegisterImageSecurityServiceServer(grpcServer, imagesecurityserviceServer.NewImageSecurityServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(observability.NewUnaryServerInterceptor(rpcTimeout, observability.SlowThreshold()))
	defer s.Stop()

	logger.LogInfo(context.Background(), "Starting rpc server at "+c.ListenOn)
	s.Start()
}
