// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ClaimBuildLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewClaimBuildLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ClaimBuildLogic {
	return &ClaimBuildLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ClaimBuildLogic) ClaimBuild(req *types.ClaimBuildReq) (resp *types.BuildEnvelope, err error) {
	result, err := l.svcCtx.Store.ClaimBuild(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.BuildEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
