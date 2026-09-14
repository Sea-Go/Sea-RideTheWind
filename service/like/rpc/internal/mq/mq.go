package main

import (
	"context"
	"flag"
	"time"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/like/common/errmsg"
	"sea-try-go/service/like/rpc/internal/model"
	"sea-try-go/service/like/rpc/internal/mq/internal/config"
	"sea-try-go/service/like/rpc/internal/mq/internal/mqs"
	"sea-try-go/service/like/rpc/internal/mq/internal/svc"

	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
)

var configFile = flag.String("f", "etc/mq.yaml", "the config file")

type OutboxRelayService struct {
	ctx    context.Context
	cancel context.CancelFunc
	svcCtx *svc.ServiceContext
	sender *mqs.LikeOutboxSender
}

func (s *OutboxRelayService) Start() {
	logger.LogInfo(s.ctx, "like outbox relay started")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			logger.LogInfo(s.ctx, "like outbox relay stopped")
			return
		case <-ticker.C:
			if err := s.sender.SendPending(s.ctx, 100); err != nil {
				logger.LogBusinessErr(s.ctx, errmsg.ErrorKafkaPush, err)
			}
		}
	}
}

func (s *OutboxRelayService) Stop() {
	s.cancel()
}

func main() {
	flag.Parse()

	var c config.Config

	conf.MustLoad(*configFile, &c)

	ctx := svc.NewServiceContext(c)

	logger.Init(c.Name)

	model.InitDB(c.DB.DataSource)

	backgroundCtx := context.Background()

	serviceGroup := service.NewServiceGroup()

	defer serviceGroup.Stop()

	consumer := kq.MustNewQueue(c.Kafka, mqs.NewLikeUpdateService(backgroundCtx, ctx))

	serviceGroup.Add(consumer)

	relayCtx, cancel := context.WithCancel(backgroundCtx)
	relayService := &OutboxRelayService{
		ctx:    relayCtx,
		cancel: cancel,
		svcCtx: ctx,
		sender: mqs.NewLikeOutboxSender(ctx),
	}
	serviceGroup.Add(relayService)

	logger.LogInfo(backgroundCtx, "like mq consumer starting")

	serviceGroup.Start()
}
