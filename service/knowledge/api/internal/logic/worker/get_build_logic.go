// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetBuildLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetBuildLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetBuildLogic {
	return &GetBuildLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetBuildLogic) GetBuild(req *types.BuildPath) (resp *types.BuildEnvelope, err error) {
	result, err := l.svcCtx.Store.GetBuild(l.ctx, req.BuildId)
	if err != nil {
		return nil, err
	}
	return &types.BuildEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
