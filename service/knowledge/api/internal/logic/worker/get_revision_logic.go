// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetRevisionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetRevisionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRevisionLogic {
	return &GetRevisionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetRevisionLogic) GetRevision(req *types.RevisionPath) (resp *types.RevisionEnvelope, err error) {
	result, err := l.svcCtx.Store.GetRevision(l.ctx, req.RevisionId)
	if err != nil {
		return nil, err
	}
	return &types.RevisionEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
