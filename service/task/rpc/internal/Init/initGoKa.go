package Init

import (
	"context"
	"encoding/json"
	"log"
	"github.com/Sea-Go/Sea-RideTheWind/service/task/rpc/internal/svc"

	"github.com/IBM/sarama"
	"github.com/lovoo/goka"
	"github.com/lovoo/goka/codec"
	"github.com/zeromicro/go-zero/core/proc"
)

/*var (
	brokers  = []string{}
	group    string
	inTopic  string
	outTopic string
)*/

// 日志结构体
type LogEvent struct {
	TS      string `json:"ts"`      // 时间戳 2026-01-22T10:11:12.123Z
	Service string `json:"service"` // 服务名 like-service/video-service
	//Position string `json:"position"` // 位置
	EventID   string `json:"event_id"`   // 全局唯一事件ID，用于幂等
	UserID    string `json:"user_id"`    // 用户ID
	TraceID   string `json:"trace_id"`   // 链路追踪ID
	ArticleID string `json:"article_id"` //文章ID
	Msg       string `json:"msg"`        // 信息
}

type TaskArticleEvent struct {
	UserID string `json:"user_id"` // 用户ID
	Cur    int64  `json:"cur"`
}

type DLQEvent struct {
	RawLog     []byte `json:"raw_log"`
	FailReason string `json:"fail_reason"`
	ProcessTS  string `json:"process_ts"`
}

var (
	brokerTask          []string
	inTopicTask         string
	rawTopicTaskUser    string
	rawTopicTaskArticle string
	outTopicTaskUser    string
	outTopicTaskArticle string
	groupTask           string
	groupTaskUser       string
	groupTaskArticle    string
	//找个时间补一下DLQ
	topicDLQ   = "raw-logs.dlq"
	countTable = "like_counts"
)

func process(ctx goka.Context, msg any) { //核心处理逻辑
	raw := msg.([]byte)

	var logEvent LogEvent
	if err := json.Unmarshal(raw, &logEvent); err != nil {
		log.Println(err)
	}

	ctx.Emit(goka.Stream(rawTopicTaskUser), logEvent.UserID, raw)
	ctx.Emit(goka.Stream(rawTopicTaskArticle), logEvent.ArticleID, raw)
}

func processUserCount(ctx goka.Context, msg any) { //核心处理逻辑
	_ = msg.([]byte)

	var cur int64
	if v := ctx.Value(); v != nil {
		cur = v.(int64)
	}
	cur++
	ctx.SetValue(cur)
	ctx.Emit(goka.Stream(outTopicTaskUser), ctx.Key(), cur)
}

func processArticleCount(ctx goka.Context, msg any) { //核心处理逻辑
	raw := msg.([]byte)

	var logEvent LogEvent
	if err := json.Unmarshal(raw, &logEvent); err != nil {
		log.Println(err)
	}

	var cur int64
	if v := ctx.Value(); v != nil {
		cur = v.(int64)
	}
	cur++
	ctx.SetValue(cur)
	_val := TaskArticleEvent{
		UserID: logEvent.UserID,
		Cur:    cur,
	}
	val, err := json.Marshal(_val)
	if err != nil {
		log.Println(err)
	}
	ctx.Emit(goka.Stream(outTopicTaskArticle), ctx.Key(), val)
}

func StartTaskGoKa(svcCtx *svc.ServiceContext) {

	ctx, cancel := context.WithCancel(context.Background())

	brokerTask = svcCtx.Config.Kafka.Brokers
	groupTask = svcCtx.Config.Kafka.GroupGoKa
	groupTaskUser = svcCtx.Config.Kafka.GroupGoKaUser
	groupTaskArticle = svcCtx.Config.Kafka.GroupGoKaArticle
	inTopicTask = svcCtx.Config.Kafka.InTopic
	rawTopicTaskUser = svcCtx.Config.Kafka.RawUserTopic
	rawTopicTaskArticle = svcCtx.Config.Kafka.RawArticleTopic
	outTopicTaskUser = svcCtx.Config.Kafka.OutUserTopic
	outTopicTaskArticle = svcCtx.Config.Kafka.OutArticleTopic

	g := goka.DefineGroup(
		goka.Group(groupTask),
		goka.Input(goka.Stream(inTopicTask), new(codec.Bytes), process),
		goka.Output(goka.Stream(rawTopicTaskUser), new(codec.Bytes)),
		goka.Output(goka.Stream(rawTopicTaskArticle), new(codec.Bytes)),
	)

	cfg := goka.DefaultConfig()
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest

	//修复topic复制因子rfactor=2的问题
	tmCfg := goka.NewTopicManagerConfig()
	tmCfg.Stream.Replication = 1
	tmCfg.Table.Replication = 1

	p, err := goka.NewProcessor(brokerTask, g,
		goka.WithConsumerGroupBuilder(goka.ConsumerGroupBuilderWithConfig(cfg)),
		goka.WithTopicManagerBuilder(goka.TopicManagerBuilderWithConfig(cfg, tmCfg)),
	)

	if err != nil {
		log.Fatal(err)
	}

	go startTaskUserGoKa(svcCtx)
	go startTaskArticleGoKa(svcCtx)

	log.Printf("like aggregator started: in=%s table=%s out=%s\n", inTopicTask, countTable, outTopicTaskUser)
	if err := p.Run(ctx); err != nil {
		log.Fatal(err)
	}

	proc.AddShutdownListener(func() {
		cancel()
	})
	<-ctx.Done()
}

func startTaskUserGoKa(svcCtx *svc.ServiceContext) {
	ctx, cancel := context.WithCancel(context.Background())

	g := goka.DefineGroup(
		goka.Group(groupTaskUser),
		goka.Input(goka.Stream(rawTopicTaskUser), new(codec.Bytes), processUserCount),
		goka.Persist(new(codec.Int64)),
		goka.Output(goka.Stream(outTopicTaskUser), new(codec.Int64)),
	)

	//为了防止出现不知道的意外，原谅我写重复代码
	cfg := goka.DefaultConfig()
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	tmCfg := goka.NewTopicManagerConfig()
	tmCfg.Stream.Replication = 1
	tmCfg.Table.Replication = 1

	p, err := goka.NewProcessor(brokerTask, g,
		goka.WithConsumerGroupBuilder(goka.ConsumerGroupBuilderWithConfig(cfg)),
		goka.WithTopicManagerBuilder(goka.TopicManagerBuilderWithConfig(cfg, tmCfg)),
	)

	if err != nil {
		log.Fatal(err)
	}
	if err := p.Run(ctx); err != nil {
		log.Fatal(err)
	}
	proc.AddShutdownListener(func() {
		cancel()
	})
	<-ctx.Done()
}

func startTaskArticleGoKa(svcCtx *svc.ServiceContext) {
	ctx, cancel := context.WithCancel(context.Background())

	g := goka.DefineGroup(
		goka.Group(groupTaskArticle),
		goka.Input(goka.Stream(rawTopicTaskArticle), new(codec.Bytes), processArticleCount),
		goka.Persist(new(codec.Int64)),
		goka.Output(goka.Stream(outTopicTaskArticle), new(codec.Bytes)),
	)

	//为了防止出现不知道的意外，原谅我写重复代码
	cfg := goka.DefaultConfig()
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	tmCfg := goka.NewTopicManagerConfig()
	tmCfg.Stream.Replication = 1
	tmCfg.Table.Replication = 1

	p, err := goka.NewProcessor(brokerTask, g,
		goka.WithConsumerGroupBuilder(goka.ConsumerGroupBuilderWithConfig(cfg)),
		goka.WithTopicManagerBuilder(goka.TopicManagerBuilderWithConfig(cfg, tmCfg)),
	)

	if err != nil {
		log.Fatal(err)
	}
	if err := p.Run(ctx); err != nil {
		log.Fatal(err)
	}
	proc.AddShutdownListener(func() {
		cancel()
	})
	<-ctx.Done()
}
