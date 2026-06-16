package main

import (
	"flag"
	"fmt"
	"sea-try-go/service/common/observability"
	"sea-try-go/service/task/rpc/internal/Init"

	"sea-try-go/service/task/rpc/internal/config"
	"sea-try-go/service/task/rpc/internal/server"
	"sea-try-go/service/task/rpc/internal/svc"
	"sea-try-go/service/task/rpc/pb"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/task.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	rpcTimeout := observability.DisableNativeRpcTimeout(&c.RpcServerConf)
	ctx := svc.NewServiceContext(c)

	go Init.StartTaskKafkaRaw(ctx)
	go Init.StartTaskGoKa(ctx)
	go Init.StartTaskKafkaUserFilter(ctx)
	go Init.StartTaskKafkaArticleFilter(ctx)
	//go Init.StartInitReward(ctx)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		__.RegisterTaskServiceServer(grpcServer, server.NewTaskServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(observability.NewUnaryServerInterceptor(rpcTimeout, observability.SlowThreshold()))
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
