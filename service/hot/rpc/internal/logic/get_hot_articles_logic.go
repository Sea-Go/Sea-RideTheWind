package logic

import (
	"context"

	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/pb"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	defaultTopK = 20
	maxTopK     = 100
	redisHotKey = "hot:articles"
)

type GetHotArticlesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetHotArticlesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetHotArticlesLogic {
	return &GetHotArticlesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetHotArticlesLogic) GetHotArticles(in *pb.GetHotArticlesRequest) (*pb.GetHotArticlesResponse, error) {
	k := int64(in.TopK)
	if k <= 0 {
		k = defaultTopK
	}
	if k > maxTopK {
		k = maxTopK
	}

	// 从 Redis ZSET 读取 Top-K（降序）
	results, err := l.svcCtx.RedisClient.ZRevRangeWithScores(l.ctx, redisHotKey, 0, k-1).Result()
	if err != nil {
		if err == redis.Nil {
			// Redis key 不存在，返回空列表
			return &pb.GetHotArticlesResponse{Items: []*pb.HotArticleItem{}}, nil
		}
		l.Errorf("redis ZRevRangeWithScores failed: %v", err)
		return nil, err
	}

	items := make([]*pb.HotArticleItem, 0, len(results))
	for _, z := range results {
		memberStr, ok := z.Member.(string)
		if !ok {
			l.Slowf("redis member is not string: %v", z.Member)
			continue
		}
		items = append(items, &pb.HotArticleItem{
			ArticleId: memberStr,
			HotScore:  uint32(z.Score),
		})
	}

	return &pb.GetHotArticlesResponse{Items: items}, nil
}
