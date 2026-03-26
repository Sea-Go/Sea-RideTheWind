package main

import (
	"context"
	"flag"
	"time"

	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/core/logx"
	"sea-try-go/service/article/rpc/internal/config"
	"sea-try-go/service/article/rpc/internal/model"
	"sea-try-go/service/article/rpc/internal/mqs"
	"sea-try-go/service/article/rpc/internal/server"
	"sea-try-go/service/article/rpc/internal/svc"
	"sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/article.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	logx.MustSetup(c.Log)
	logger.Init(c.Name)

	serviceGroup := service.NewServiceGroup()
	defer serviceGroup.Stop()

	u := model.NewArticleRepo(c)
	ctx := svc.NewServiceContext(c, u)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		__.RegisterArticleServiceServer(grpcServer, server.NewArticleServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	defer s.Stop()

	serviceGroup.Add(s)

	backgroundCtx := context.Background()
	consumer := mqs.NewArticleConsumer(backgroundCtx, ctx)
	serviceGroup.Add(kq.MustNewQueue(c.KqConsumerConf, consumer))
	resultConsumer := mqs.NewArticleSyncResultConsumer(backgroundCtx, ctx)
	serviceGroup.Add(kq.MustNewQueue(c.ArticleSyncResultConsumerConf, resultConsumer))

	relayInterval := time.Duration(c.ArticleSyncOutbox.PollIntervalMs) * time.Millisecond
	if relayInterval <= 0 {
		relayInterval = time.Second
	}
	batchSize := c.ArticleSyncOutbox.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	serviceGroup.Add(&articleSyncRelayService{
		ctx:       backgroundCtx,
		interval:  relayInterval,
		batchSize: batchSize,
		sender:    mqs.NewArticleSyncOutboxSender(ctx),
	})

	logx.Infof("starting article rpc server at %s", c.ListenOn)
	logx.Infof("starting article review consumer on topic %s", c.KqConsumerConf.Topic)
	logx.Infof("starting article sync result consumer on topic %s", c.ArticleSyncResultConsumerConf.Topic)
	serviceGroup.Start()
}

type articleSyncRelayService struct {
	ctx       context.Context
	interval  time.Duration
	batchSize int
	sender    *mqs.ArticleSyncOutboxSender
	cancel    context.CancelFunc
}

func (s *articleSyncRelayService) Start() {
	relayCtx, cancel := context.WithCancel(s.ctx)
	s.cancel = cancel
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-relayCtx.Done():
			return
		case <-ticker.C:
			if err := s.sender.SendPending(relayCtx, s.batchSize); err != nil {
				logx.Errorf("send article sync outbox failed: %v", err)
			}
		}
	}
}

func (s *articleSyncRelayService) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}
