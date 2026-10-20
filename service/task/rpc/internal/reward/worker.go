package reward

/*import (
	"context"
	"log"
	pointspb "github.com/Sea-Go/Sea-RideTheWind/service/points/rpc/pb"
	"github.com/Sea-Go/Sea-RideTheWind/service/task/rpc/internal/svc"
	//userpb "github.com/Sea-Go/Sea-RideTheWind/service/user/rpc/pb"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Worker struct {
	rdb          *redis.Client
	points       pointspb.PointsServiceClient
	consumerName string

	batch    int64
	blockDur time.Duration

	failSleep time.Duration
}

func NewWorker(rdb *redis.Client, points pointspb.PointsServiceClient, consumerName string) *Worker {
	return &Worker{
		rdb:          rdb,
		points:       points,
		consumerName: consumerName,
		batch:        64,
		blockDur:     5 * time.Second,
		failSleep:    300 * time.Millisecond,
	}
}

func (w *Worker) EnsureGroup(ctx context.Context) error {
	err := w.rdb.XGroupCreateMkStream(ctx, StreamKey, GroupName, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

func (w *Worker) Run(ctx context.Context, svc *svc.ServiceContext) error {
	if err := w.EnsureGroup(ctx); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		res, err := w.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    GroupName,
			Consumer: w.consumerName,
			Streams:  []string{StreamKey, ">"},
			Count:    w.batch,
			Block:    w.blockDur,
		}).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			time.Sleep(w.failSleep)
			continue
		}
		for _, s := range res {
			for _, msg := range s.Messages {
				ev := parseRedisEvent(msg.Values)
				userResp, err := svc.UserClient.GetUser(ctx, &userpb.GetUserReq{Id: uint64(ev.UID)})
				if err != nil {
					log.Println(err)
				}
				req := &pointspb.AddPointsReq{
					UserId:     ev.UID,
					UserPoints: int64(userResp.User.Score),
					RequestId:  ev.RewardID,
					AddPoints:  ev.AddScore,
				}
				if _, err := w.points.AddPoints(ctx, req); err != nil {
					//留给后边重试
					continue
				}
				_ = w.rdb.XAck(ctx, StreamKey, GroupName, msg.ID).Err()
				_ = w.rdb.XDel(ctx, StreamKey, msg.ID).Err()
			}
		}
	}
}

func parseRedisEvent(values map[string]any) RedisEvent {
	getI64 := func(k string) int64 {
		if v, ok := values[k]; ok {
			switch x := v.(type) {
			case int64:
				return x
			case int:
				return int64(x)
			case string:
				n, _ := strconv.ParseInt(x, 10, 64)
				return n
			case []byte:
				n, _ := strconv.ParseInt(string(x), 10, 64)
				return n
			default:
				return 0
			}
		}
		return 0
	}

	return RedisEvent{
		RewardID: getI64("reward_id"),
		UID:      getI64("uid"),
		TaskID:   getI64("task_id"),
		Ts:       getI64("ts"),
		AddScore: getI64("add_score"),
	}
}
*/
