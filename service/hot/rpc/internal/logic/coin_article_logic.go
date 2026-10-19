package logic

import (
	"context"

	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CoinArticleLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCoinArticleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CoinArticleLogic {
	return &CoinArticleLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CoinArticle 投币文章（预留接口）
// 投币仅作为互动指标之一参与热度计算，不涉及积分扣减。
// 实现时需要：
// 1. 更新 PostgreSQL article 相关计数
// 2. 发布 ArticleHotEvent{ArticleID, Type: "coin"} 到 Kafka
//    权重由热点系统配置决定，业务侧无需关心
func (l *CoinArticleLogic) CoinArticle(in *pb.CoinArticleRequest) (*pb.CoinArticleResponse, error) {
	// TODO: 投币系统实现后补充
	return nil, status.Error(codes.Unimplemented, "CoinArticle not implemented yet")
}
