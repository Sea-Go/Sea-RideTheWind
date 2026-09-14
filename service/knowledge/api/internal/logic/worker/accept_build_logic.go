// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AcceptBuildLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAcceptBuildLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AcceptBuildLogic {
	return &AcceptBuildLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AcceptBuildLogic) AcceptBuild(req *types.AcceptBuildReq) (resp *types.BuildEnvelope, err error) {
	result, err := l.svcCtx.Store.AcceptBuild(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.BuildEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
