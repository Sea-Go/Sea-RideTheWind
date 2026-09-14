// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"
	"sea-try-go/service/knowledge/api/internal/middleware"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateSourceLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateSourceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateSourceLogic {
	return &CreateSourceLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateSourceLogic) CreateSource(req *types.CreateSourceReq) (resp *types.RevisionEnvelope, err error) {
	actor := middleware.Actor(l.ctx.Value("userId"))
	result, err := l.svcCtx.Store.CreateSource(l.ctx, actor, *req)
	if err != nil {
		return nil, err
	}
	return &types.RevisionEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
