package main

import (
	"flag"
	"log/slog"
	"os"

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
	if c.SubjectRefV2Facts && c.Mode != service.DevMode && c.Mode != service.TestMode {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("favorite v2 fact candidate denied",
			"event", "favorite.subject_ref_v2.startup_rejected", "error_code", "LOCAL_CANDIDATE_ONLY")
		os.Exit(2)
	}
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

	slog.New(slog.NewJSONHandler(os.Stderr, nil)).Info("favorite rpc starting",
		"event", "favorite.rpc.starting", "listen_on", c.ListenOn,
		"subject_ref_version", favoriteSubjectVersion(c.SubjectRefV2Facts))
	s.Start()
}

func favoriteSubjectVersion(v2 bool) string {
	if v2 {
		return "v2"
	}
	return "v1"
}
