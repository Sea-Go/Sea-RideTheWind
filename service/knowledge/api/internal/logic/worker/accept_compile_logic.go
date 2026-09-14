// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AcceptCompileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAcceptCompileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AcceptCompileLogic {
	return &AcceptCompileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AcceptCompileLogic) AcceptCompile(req *types.AcceptCompileReq) (resp *types.CompileEnvelope, err error) {
	result, err := l.svcCtx.Store.AcceptCompile(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.CompileEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
