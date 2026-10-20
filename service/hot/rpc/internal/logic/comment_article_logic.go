package logic

import (
	"context"

	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type CommentArticleLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCommentArticleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CommentArticleLogic {
	return &CommentArticleLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CommentArticle 评论触发热度更新（预留接口）
// 方案A：评论系统自行推送 ArticleHotEvent 到 Kafka article-hot-events topic
// 此 RPC 当前返回 success，待评论系统完成推送后可作为同步调用备选
func (l *CommentArticleLogic) CommentArticle(in *pb.CommentArticleRequest) (*pb.CommentArticleResponse, error) {
	return &pb.CommentArticleResponse{Success: true}, nil
}
