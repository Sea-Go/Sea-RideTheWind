// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ClaimCompileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewClaimCompileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ClaimCompileLogic {
	return &ClaimCompileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ClaimCompileLogic) ClaimCompile(req *types.ClaimCompileReq) (resp *types.CompileEnvelope, err error) {
	result, err := l.svcCtx.Store.ClaimCompile(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.CompileEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
