// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package article

import (
	"context"
	"fmt"

	"sea-try-go/service/article/api/internal/svc"
	"sea-try-go/service/article/api/internal/types"
	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/article/rpc/articleservice"
	"sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UpdateArticleLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateArticleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateArticleLogic {
	return &UpdateArticleLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateArticleLogic) UpdateArticle(req *types.UpdateArticleReq) (resp *types.UpdateArticleResp, code int) {
	currentUserID, code := extractCurrentUserID(l.ctx)
	if code != errmsg.Success {
		logger.LogBusinessErr(l.ctx, code, fmt.Errorf("missing login userId in article update context"))
		return nil, code
	}

	articleResp, err := l.svcCtx.ArticleRpc.GetArticle(l.ctx, &articleservice.GetArticleRequest{
		ArticleId: req.ArticleId,
		IncrView:  false,
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
	if articleResp == nil || articleResp.Article == nil {
		return nil, errmsg.ErrorArticleNone
	}
	if articleResp.Article.AuthorId != currentUserID {
		return nil, errmsg.ErrorArticleForbidden
	}

	rpcReq := &articleservice.UpdateArticleRequest{
		ArticleId:     req.ArticleId,
		SecondaryTags: req.SecondaryTags,
	}

	if req.Title != "" {
		rpcReq.Title = &req.Title
	}
	if req.Brief != "" {
		rpcReq.Brief = &req.Brief
	}
	if req.Content != "" {
		rpcReq.MarkdownContent = &req.Content
	}
	if req.CoverImageUrl != "" {
		rpcReq.CoverImageUrl = &req.CoverImageUrl
	}
	if req.ManualTypeTag != "" {
		rpcReq.ManualTypeTag = &req.ManualTypeTag
	}
	if req.Status != 0 {
		status := __.ArticleStatus(req.Status)
		rpcReq.Status = status.Enum()
	}

	rpcResp, err := l.svcCtx.ArticleRpc.UpdateArticle(l.ctx, rpcReq)
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

	return &types.UpdateArticleResp{
		Success: rpcResp.GetSuccess(),
	}, errmsg.Success
}
