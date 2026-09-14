// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetModuleRevisionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetModuleRevisionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetModuleRevisionLogic {
	return &GetModuleRevisionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetModuleRevisionLogic) GetModuleRevision(req *types.ModuleRevisionPath) (resp *types.RevisionEnvelope, err error) {
	result, err := l.svcCtx.Store.GetModuleRevision(l.ctx, req.ModuleId, req.RevisionId)
	if err != nil {
		return nil, err
	}
	return &types.RevisionEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
