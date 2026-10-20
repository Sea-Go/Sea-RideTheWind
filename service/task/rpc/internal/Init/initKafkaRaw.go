package Init

import (
	"context"
	"github.com/Sea-Go/Sea-RideTheWind/service/task/rpc/internal/svc"

	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/core/proc"
)

// 这个是用来测试哪一步的kafka死掉了
type PrintConsumer struct {
}

func (c PrintConsumer) Consume(ctx context.Context, key string, val string) error {
	//log.Printf("got msg key=%s val=%s\n", key, val)
	return nil
}

func StartTaskKafkaRaw(svcCtx *svc.ServiceContext) {
	ctx, cancel := context.WithCancel(context.Background())

	brokers := svcCtx.Config.Kafka.Brokers
	topic := svcCtx.Config.Kafka.InTopic
	group := svcCtx.Config.Kafka.GroupKafkaRaw

	q := kq.MustNewQueue(kq.KqConf{
		Brokers:    brokers,
		Topic:      topic,
		Group:      group,
		Consumers:  1,
		Offset:     "latest",
		Processors: 1,
	}, PrintConsumer{})

	// 退出时停掉 consumer
	proc.AddShutdownListener(func() {
		cancel()
		q.Stop()
	})

	q.Start()
	<-ctx.Done()
}
