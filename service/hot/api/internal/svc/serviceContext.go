// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package svc

import (
	"context"

	"sea-try-go/service/article/rpc/articleservice"
	"sea-try-go/service/hot/api/internal/config"
	hotpb "sea-try-go/service/hot/rpc/pb"

	"github.com/zeromicro/go-zero/zrpc"
)

type HotRPC interface {
	GetHotArticles(ctx context.Context, in *hotpb.GetHotArticlesRequest) (*hotpb.GetHotArticlesResponse, error)
}

type ArticleRPC interface {
	GetArticle(ctx context.Context, in *articleservice.GetArticleRequest) (*articleservice.GetArticleResponse, error)
}

type ServiceContext struct {
	Config     config.Config
	HotRpc     HotRPC
	ArticleRpc ArticleRPC
}

func NewServiceContext(c config.Config) *ServiceContext {
	hotClient := zrpc.MustNewClient(c.HotRpcConf)
	articleClient := zrpc.MustNewClient(c.ArticleRpcConf)

	return &ServiceContext{
		Config:     c,
		HotRpc:     hotRPCAdapter{client: hotpb.NewHotServiceClient(hotClient.Conn())},
		ArticleRpc: articleRPCAdapter{client: articleservice.NewArticleService(articleClient)},
	}
}

type hotRPCAdapter struct {
	client hotpb.HotServiceClient
}

func (h hotRPCAdapter) GetHotArticles(ctx context.Context, in *hotpb.GetHotArticlesRequest) (*hotpb.GetHotArticlesResponse, error) {
	return h.client.GetHotArticles(ctx, in)
}

type articleRPCAdapter struct {
	client articleservice.ArticleService
}

func (a articleRPCAdapter) GetArticle(ctx context.Context, in *articleservice.GetArticleRequest) (*articleservice.GetArticleResponse, error) {
	return a.client.GetArticle(ctx, in)
}

var _ HotRPC = hotRPCAdapter{}
var _ ArticleRPC = articleRPCAdapter{}
