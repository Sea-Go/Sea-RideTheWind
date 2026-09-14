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

type WithdrawLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewWithdrawLogic(ctx context.Context, svcCtx *svc.ServiceContext) *WithdrawLogic {
	return &WithdrawLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *WithdrawLogic) Withdraw(req *types.WithdrawReq) (resp *types.ReceiptEnvelope, err error) {
	actor := middleware.Actor(l.ctx.Value("userId"))
	result, err := l.svcCtx.Store.Withdraw(l.ctx, actor, *req)
	if err != nil {
		return nil, err
	}
	return &types.ReceiptEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
