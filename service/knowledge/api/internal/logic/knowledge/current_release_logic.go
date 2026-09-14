// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type CurrentReleaseLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCurrentReleaseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CurrentReleaseLogic {
	return &CurrentReleaseLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CurrentReleaseLogic) CurrentRelease(req *types.ModulePath) (resp *types.ReleaseStateEnvelope, err error) {
	result, err := l.svcCtx.Store.Current(l.ctx, req.ModuleId)
	if err != nil {
		return nil, err
	}
	return &types.ReleaseStateEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
