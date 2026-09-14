// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package article

import (
	"context"
	"sea-try-go/service/article/common/errmsg"

	"sea-try-go/service/article/api/internal/svc"
	"sea-try-go/service/article/api/internal/types"
	"sea-try-go/service/article/rpc/articleservice"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetAuthorArticleLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetAuthorArticleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAuthorArticleLogic {
	return &GetAuthorArticleLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAuthorArticleLogic) GetAuthorArticle(req *types.GetArticleReq) (*types.GetArticleResp, int) {
	authorID, code := extractCurrentUserID(l.ctx)
	if code != errmsg.Success {
		return nil, code
	}
	res, err := l.svcCtx.ArticleRpc.GetArticle(l.ctx, &articleservice.GetArticleRequest{
		ArticleId: req.ArticleId, RequesterId: authorID,
	})
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.NotFound:
			return nil, errmsg.ErrorArticleNone
		case codes.PermissionDenied:
			return nil, errmsg.ErrorArticleForbidden
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		default:
			return nil, errmsg.CodeServerBusy
		}
	}
	if res == nil || res.Article == nil {
		return nil, errmsg.ErrorArticleNone
	}
	a := res.Article
	// Keep the HTTP authorization decision even while RPC instances are
	// upgraded; an older RPC ignores requester_id.
	if a.AuthorId != authorID {
		return nil, errmsg.ErrorArticleForbidden
	}
	response := &types.GetArticleResp{Article: types.Article{
		Id: a.Id, Title: a.Title, Brief: a.Brief, Content: a.MarkdownContent,
		CoverImageUrl: a.CoverImageUrl, ManualTypeTag: a.ManualTypeTag,
		SecondaryTags: a.SecondaryTags, AuthorId: a.AuthorId,
		CreateTime: a.CreateTime, UpdateTime: a.UpdateTime,
		Status: int32(a.Status), ViewCount: a.ViewCount,
		LikeCount: a.LikeCount, CommentCount: a.CommentCount,
		ShareCount: a.ShareCount, ExtInfo: cloneStringMap(a.ExtInfo),
	}}
	enrichArticleAuthor(l.ctx, l.svcCtx, &response.Article)
	return response, errmsg.Success
}
