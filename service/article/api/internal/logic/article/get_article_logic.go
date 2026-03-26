// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package article

import (
	"context"
	"sea-try-go/service/article/rpc/articleservice"

	"sea-try-go/service/article/api/internal/svc"
	"sea-try-go/service/article/api/internal/types"
	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/common/logger"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetArticleLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetArticleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetArticleLogic {
	return &GetArticleLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetArticleLogic) GetArticle(req *types.GetArticleReq) (resp *types.GetArticleResp, code int) {
	res, err := l.svcCtx.ArticleRpc.GetArticle(l.ctx, &articleservice.GetArticleRequest{
		ArticleId: req.ArticleId,
		IncrView:  req.IncrView,
	})
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.NotFound:
			return nil, errmsg.ErrorArticleNone
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		default:
			return nil, errmsg.CodeServerBusy
		}
	}

	if res.Article == nil {
		return nil, errmsg.ErrorArticleNone
	}

	resp = &types.GetArticleResp{
		Article: types.Article{
			Id:            res.Article.Id,
			Title:         res.Article.Title,
			Brief:         res.Article.Brief,
			Content:       res.Article.MarkdownContent,
			CoverImageUrl: res.Article.CoverImageUrl,
			AuthorId:      res.Article.AuthorId,
			CreateTime:    res.Article.CreateTime,
			UpdateTime:    res.Article.UpdateTime,
			Status:        int32(res.Article.Status),
			ManualTypeTag: res.Article.ManualTypeTag,
			SecondaryTags: res.Article.SecondaryTags,
			ViewCount:     res.Article.ViewCount,
			LikeCount:     res.Article.LikeCount,
			CommentCount:  res.Article.CommentCount,
			ShareCount:    res.Article.ShareCount,
			ExtInfo:       cloneStringMap(res.Article.ExtInfo),
		},
	}
	enrichArticleAuthor(l.ctx, l.svcCtx, &resp.Article)
	return resp, errmsg.Success
}
