package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"sea-try-go/service/common/observability"
	"sea-try-go/service/hot/rpc/internal/config"
	"sea-try-go/service/hot/rpc/internal/mqs"
	"sea-try-go/service/hot/rpc/internal/server"
	"sea-try-go/service/hot/rpc/internal/svc"
	"sea-try-go/service/hot/rpc/pb"

	"github.com/IBM/sarama"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/hot.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	rpcTimeout := observability.DisableNativeRpcTimeout(&c.RpcServerConf)
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterHotServiceServer(grpcServer, server.NewHotServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(observability.NewUnaryServerInterceptor(rpcTimeout, observability.SlowThreshold()))

	serviceGroup := service.NewServiceGroup()
	serviceGroup.Add(s)

	weights := make(map[string]int32, len(c.Interaction.Weights))
	for _, w := range c.Interaction.Weights {
		weights[w.Name] = w.Weight
	}

	hotTTL := time.Duration(c.Interaction.TTL) * time.Second
	handler := mqs.NewHotHandler(ctx, c.Interaction.SyncEvery, hotTTL, weights, c.KqConsumerConf.Topic)

	saramaConf := sarama.NewConfig()
	saramaConf.Version = sarama.V2_6_0_0
	saramaConf.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRoundRobin()
	saramaConf.Consumer.Offsets.Initial = sarama.OffsetNewest

	cg, err := sarama.NewConsumerGroup(c.KqConsumerConf.Brokers, c.KqConsumerConf.Group, saramaConf)
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			if err := cg.Consume(context.Background(), []string{c.KqConsumerConf.Topic}, handler); err != nil {
				logx.Errorf("Consumer error: %v", err)
			}
		}
	}()

	fmt.Printf("Starting hot rpc server at %s...\n", c.ListenOn)
	serviceGroup.Start()
}
