// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package article

import (
	"context"

	"sea-try-go/service/article/api/internal/svc"
	"sea-try-go/service/article/api/internal/types"
	"sea-try-go/service/article/rpc/articleservice"

	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/common/logger"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ListArticlesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListArticlesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListArticlesLogic {
	return &ListArticlesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListArticlesLogic) ListArticles(req *types.ListArticlesReq) (resp *types.ListArticlesResp, code int) {
	res, err := l.svcCtx.ArticleRpc.ListArticles(l.ctx, &articleservice.ListArticlesRequest{
		ManualTypeTag: &req.ManualTypeTag,
		SecondaryTag:  &req.SecondaryTag,
		RelatedGameId: &req.RelatedGameId,
		AuthorId:      &req.AuthorId,
		Page:          req.Page,
		PageSize:      req.PageSize,
		SortBy:        req.SortBy,
		Desc:          req.Desc,
	})
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		default:
			return nil, errmsg.CodeServerBusy
		}
	}

	var articles []types.Article
	for _, item := range res.Articles {
		articles = append(articles, types.Article{
			Id:            item.Id,
			Title:         item.Title,
			Brief:         item.Brief,
			Content:       item.MarkdownContent,
			CoverImageUrl: item.CoverImageUrl,
			ManualTypeTag: item.ManualTypeTag,
			SecondaryTags: item.SecondaryTags,
			AuthorId:      item.AuthorId,
			CreateTime:    item.CreateTime,
			UpdateTime:    item.UpdateTime,
			Status:        int32(item.Status),
			ViewCount:     item.ViewCount,
			LikeCount:     item.LikeCount,
			CommentCount:  item.CommentCount,
			ShareCount:    item.ShareCount,
			ExtInfo:       cloneStringMap(item.ExtInfo),
		})
	}
	enrichArticleAuthors(l.ctx, l.svcCtx, articles)

	return &types.ListArticlesResp{
		Articles: articles,
		Total:    res.Total,
		Page:     res.Page,
		PageSize: res.PageSize,
	}, errmsg.Success
}
