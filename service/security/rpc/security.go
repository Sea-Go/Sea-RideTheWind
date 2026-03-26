package main

import (
	"context"
	"flag"

	"sea-try-go/service/security/rpc/internal/config"
	contentsecurityserviceServer "sea-try-go/service/security/rpc/internal/server/contentsecurityservice"
	imagesecurityserviceServer "sea-try-go/service/security/rpc/internal/server/imagesecurityservice"
	"sea-try-go/service/security/rpc/internal/svc"
	"sea-try-go/service/security/rpc/pb/sea-try-go/service/security/rpc/pb"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"sea-try-go/service/common/logger"
)

var configFile = flag.String("f", "etc/security.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx := svc.NewServiceContext(c)

	logger.Init("security-rpc")

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterContentSecurityServiceServer(grpcServer, contentsecurityserviceServer.NewContentSecurityServiceServer(ctx))
		pb.RegisterImageSecurityServiceServer(grpcServer, imagesecurityserviceServer.NewImageSecurityServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	defer s.Stop()

	logger.LogInfo(context.Background(), "Starting rpc server at "+c.ListenOn)
	s.Start()
}
