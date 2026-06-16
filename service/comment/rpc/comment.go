package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"sea-try-go/service/comment/rpc/internal/config"
	"sea-try-go/service/comment/rpc/internal/metrics"
	"sea-try-go/service/comment/rpc/internal/mqs"
	"sea-try-go/service/comment/rpc/internal/server"
	"sea-try-go/service/comment/rpc/internal/svc"
	"sea-try-go/service/comment/rpc/pb"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/observability"

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
