package logic

import (
	"context"

	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LikeArticleLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLikeArticleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LikeArticleLogic {
	return &LikeArticleLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// LikeArticle 点赞文章（预留接口）
// 实现时需要：
//  1. 更新 PostgreSQL article.like_count
//  2. 发布 ArticleHotEvent 到 Kafka topic "article-hot-events"
//     业务侧只需发送 article_id + type，权重由热点系统配置决定
//
// 示例发布逻辑：
//
//	event := mqs.ArticleHotEvent{
//	    ArticleID: in.ArticleId,
//	    Type:      "like",
//	}
//	payload, _ := json.Marshal(event)
//	svcCtx.KqPusher.Push(ctx, string(payload))
func (l *LikeArticleLogic) LikeArticle(in *pb.LikeArticleRequest) (*pb.LikeArticleResponse, error) {
	// TODO: 点赞系统实现后补充
	return nil, status.Error(codes.Unimplemented, "LikeArticle not implemented yet")
}
