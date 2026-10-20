package server

import (
	"context"

	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/internal/logic"
	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/pb"
)

type HotServiceServer struct {
	svcCtx *svc.ServiceContext
	pb.UnimplementedHotServiceServer
}

func NewHotServiceServer(svcCtx *svc.ServiceContext) *HotServiceServer {
	return &HotServiceServer{
		svcCtx: svcCtx,
	}
}

func (s *HotServiceServer) GetHotArticles(ctx context.Context, in *pb.GetHotArticlesRequest) (*pb.GetHotArticlesResponse, error) {
	l := logic.NewGetHotArticlesLogic(ctx, s.svcCtx)
	return l.GetHotArticles(in)
}

// 互动接口（预留，点赞/评论/投币/分享系统实现后补充逻辑）
func (s *HotServiceServer) LikeArticle(ctx context.Context, in *pb.LikeArticleRequest) (*pb.LikeArticleResponse, error) {
	l := logic.NewLikeArticleLogic(ctx, s.svcCtx)
	return l.LikeArticle(in)
}

func (s *HotServiceServer) CommentArticle(ctx context.Context, in *pb.CommentArticleRequest) (*pb.CommentArticleResponse, error) {
	l := logic.NewCommentArticleLogic(ctx, s.svcCtx)
	return l.CommentArticle(in)
}

func (s *HotServiceServer) CoinArticle(ctx context.Context, in *pb.CoinArticleRequest) (*pb.CoinArticleResponse, error) {
	l := logic.NewCoinArticleLogic(ctx, s.svcCtx)
	return l.CoinArticle(in)
}

func (s *HotServiceServer) ShareArticle(ctx context.Context, in *pb.ShareArticleRequest) (*pb.ShareArticleResponse, error) {
	l := logic.NewShareArticleLogic(ctx, s.svcCtx)
	return l.ShareArticle(in)
}
