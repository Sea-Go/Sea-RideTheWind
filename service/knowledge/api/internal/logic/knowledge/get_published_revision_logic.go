// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetPublishedRevisionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetPublishedRevisionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetPublishedRevisionLogic {
	return &GetPublishedRevisionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetPublishedRevisionLogic) GetPublishedRevision(req *types.PublishedRevisionPath) (resp *types.RevisionEnvelope, err error) {
	result, err := l.svcCtx.Store.GetPublishedRevision(l.ctx, req.ModuleId, req.ReleaseId, req.RevisionId)
	if err != nil {
		return nil, err
	}
	return &types.RevisionEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
