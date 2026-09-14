// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetCurrentSearchSnapshotLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetCurrentSearchSnapshotLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetCurrentSearchSnapshotLogic {
	return &GetCurrentSearchSnapshotLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetCurrentSearchSnapshotLogic) GetCurrentSearchSnapshot(req *types.ModulePath) (resp *types.SearchSnapshotEnvelope, err error) {
	snapshot, err := l.svcCtx.Store.GetCurrentSearchSnapshot(l.ctx, req.ModuleId)
	if err != nil {
		return nil, err
	}
	return &types.SearchSnapshotEnvelope{Code: 200, Msg: "success", Data: snapshot}, nil
}
