// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package article

import (
	"context"
	"fmt"
	"sea-try-go/service/article/rpc/articleservice"
	"strings"
	"time"

	"sea-try-go/service/article/api/internal/svc"
	"sea-try-go/service/article/api/internal/types"
	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/common/logger"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateArticleLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateArticleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateArticleLogic {
	return &CreateArticleLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateArticleLogic) CreateArticle(req *types.CreateArticleReq) (resp *types.CreateArticleResp, code int) {
	authorId, code := extractCurrentUserID(l.ctx)
	if code != errmsg.Success {
		logger.LogBusinessErr(l.ctx, code, fmt.Errorf("missing login userId in article create context"))
		return nil, code
	}

	rpcResp, err := l.svcCtx.ArticleRpc.CreateArticle(l.ctx, &articleservice.CreateArticleRequest{
		Title:           req.Title,
		Brief:           &req.Brief,
		MarkdownContent: req.Content,
		CoverImageUrl:   &req.CoverImageUrl,
		ManualTypeTag:   req.ManualTypeTag,
		SecondaryTags:   req.SecondaryTags,
		AuthorId:        authorId,
	}, zrpc.WithCallTimeout(70*time.Second))

	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		articleID := extractArticleIDFromStatusMessage(st.Message())
		switch st.Code() {
		case codes.AlreadyExists:
			return nil, errmsg.ErrorArticleExist
		case codes.FailedPrecondition:
			return &types.CreateArticleResp{
				ArticleId: articleID,
			}, errmsg.ErrorArticlePublishFailed
		case codes.DeadlineExceeded:
			return &types.CreateArticleResp{
				ArticleId: articleID,
			}, errmsg.ErrorArticlePublishing
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		default:
			return nil, errmsg.CodeServerBusy
		}
	}

	return &types.CreateArticleResp{
		ArticleId: rpcResp.ArticleId,
	}, errmsg.Success
}

func extractArticleIDFromStatusMessage(message string) string {
	if message == "" {
		return ""
	}

	idx := strings.LastIndex(message, ":")
	if idx < 0 || idx+1 >= len(message) {
		return ""
	}
	return strings.TrimSpace(message[idx+1:])
}
