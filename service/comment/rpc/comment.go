package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/config"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/mqs"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/server"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/pb"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/observability"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/comment.yaml", "the config file")

func main() {
	flag.Parse()
	var c config.Config
	conf.MustLoad(*configFile, &c)
	logger.Init(c.Name)
	rpcTimeout := observability.DisableNativeRpcTimeout(&c.RpcServerConf)
	ctx := svc.NewServiceContext(c)
	bgCtx := context.Background()
	metrics.InitMetrics(&c)

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		_ = http.ListenAndServe(":9091", nil)
	}()

	queue := kq.MustNewQueue(c.KqConsumerConf, mqs.NewAuditConsumer(bgCtx, ctx))
	go queue.Start()
	defer queue.Stop()

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterCommentServiceServer(grpcServer, server.NewCommentServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(observability.NewUnaryServerInterceptor(rpcTimeout, observability.SlowThreshold()))
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
