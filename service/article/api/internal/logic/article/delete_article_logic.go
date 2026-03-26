// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package article

import (
	"context"
	"fmt"
	"sea-try-go/service/article/rpc/articleservice"

	"sea-try-go/service/article/api/internal/svc"
	"sea-try-go/service/article/api/internal/types"
	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/common/logger"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DeleteArticleLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteArticleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteArticleLogic {
	return &DeleteArticleLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteArticleLogic) DeleteArticle(req *types.DeleteArticleReq) (resp *types.DeleteArticleResp, code int) {
	operatorID, code := extractCurrentUserID(l.ctx)
	if code != errmsg.Success {
		logger.LogBusinessErr(l.ctx, code, fmt.Errorf("missing login userId in article delete context"))
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
	if articleResp.Article.AuthorId != operatorID {
		return nil, errmsg.ErrorArticleForbidden
	}

	_, err = l.svcCtx.ArticleRpc.DeleteArticle(l.ctx, &articleservice.DeleteArticleRequest{
		ArticleId:  req.ArticleId,
		OperatorId: operatorID,
	})
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
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

	return &types.DeleteArticleResp{
		Success: true,
	}, errmsg.Success
}
