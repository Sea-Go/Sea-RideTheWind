package logic

import (
	"context"

	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ShareArticleLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewShareArticleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ShareArticleLogic {
	return &ShareArticleLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ShareArticle 分享文章（预留接口）
// 实现时需要：
//  1. 记录分享渠道（wechat / weibo / qq / link）
//  2. 更新 PostgreSQL article.share_count
//  3. 发布 ArticleHotEvent{ArticleID, Type: "share"} 到 Kafka
//     权重由热点系统配置决定，业务侧无需关心
func (l *ShareArticleLogic) ShareArticle(in *pb.ShareArticleRequest) (*pb.ShareArticleResponse, error) {
	// TODO: 分享系统实现后补充
	return nil, status.Error(codes.Unimplemented, "ShareArticle not implemented yet")
}
