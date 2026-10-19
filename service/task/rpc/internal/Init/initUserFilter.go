package Init

import (
	"context"
	"log"
	"github.com/Sea-Go/Sea-RideTheWind/service/task/rpc/internal/sink"
	"github.com/Sea-Go/Sea-RideTheWind/service/task/rpc/internal/svc"

	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/core/proc"
)

/*type PrintFilterConsumer struct {
}

func (c PrintFilterConsumer) Consume(ctx context.Context, key string, val string) error {
	log.Printf("got msg key=%s val=%s\n", key, val)

	return nil
}*/

func StartTaskKafkaUserFilter(svcCtx *svc.ServiceContext) {

	log.Println("start task kafka filter")

	ctx, cancel := context.WithCancel(context.Background())

	brokers := svcCtx.Config.Kafka.Brokers
	topic := svcCtx.Config.Kafka.OutUserTopic
	group := svcCtx.Config.Kafka.GroupFilterUser

	consumer := sink.NewUserLikeSinkConsumer(svcCtx.Rdb, svcCtx.Gdb)
	consumer.Start(ctx) //异步二级存储

	/*q := kq.MustNewQueue(kq.KqConf{
		Brokers:    brokers,
		Topic:      topic,
		Group:      group,
		Consumers:  1,
		Offset:     "latest",
		Processors: 1,
	}, PrintFilterConsumer{})*/

	q := kq.MustNewQueue(kq.KqConf{
		Brokers:    brokers,
		Topic:      topic,
		Group:      group,
		Consumers:  1,
		Offset:     "latest",
		Processors: 1,
	}, consumer)

	proc.AddShutdownListener(func() {
		q.Stop()
		cancel()
	})
	q.Start()
	<-ctx.Done()
}
