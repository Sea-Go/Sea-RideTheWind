// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListPublishedRevisionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListPublishedRevisionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListPublishedRevisionsLogic {
	return &ListPublishedRevisionsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListPublishedRevisionsLogic) ListPublishedRevisions(req *types.PublishedRevisionsReq) (resp *types.ListRevisionsRespEnvelope, err error) {
	result, err := l.svcCtx.Store.ListPublishedRevisions(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.ListRevisionsRespEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
