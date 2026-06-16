package main

import (
	"flag"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/observability"
	"sea-try-go/service/points/rpc/internal/config"
	"sea-try-go/service/points/rpc/internal/metrics"
	"sea-try-go/service/points/rpc/internal/mqs"
	"sea-try-go/service/points/rpc/internal/server"
	"sea-try-go/service/points/rpc/internal/svc"
	__ "sea-try-go/service/points/rpc/pb"

	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/points.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	rpcTimeout := observability.DisableNativeRpcTimeout(&c.RpcServerConf)
	ctx := svc.NewServiceContext(c)
	logger.Init("points-rpc")
	metrics.InitMetrics(&c)
	serviceGroup := service.NewServiceGroup()
	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		__.RegisterPointsServiceServer(grpcServer, server.NewPointsServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(observability.NewUnaryServerInterceptor(rpcTimeout, observability.SlowThreshold()))
	serviceGroup.Add(s)

	consumer := kq.MustNewQueue(c.KqConsumerConf, mqs.NewPointsHandler(ctx))
	serviceGroup.Add(consumer)

	retryHandler := mqs.NewRetryHandler(ctx)
	go ctx.RetryDqConsumer.Consume(retryHandler.Consume)

	delayHandler := mqs.NewDelayHandler(ctx)
	go ctx.DqConsumer.Consume(delayHandler.Consume)

	serviceGroup.Start()
}
