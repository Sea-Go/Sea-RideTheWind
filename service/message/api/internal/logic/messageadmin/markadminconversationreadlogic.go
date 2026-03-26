// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package messageadmin

import (
	"context"
	"time"

	"sea-try-go/service/message/api/internal/logic/shared"
	"sea-try-go/service/message/api/internal/metrics"
	"sea-try-go/service/message/api/internal/svc"
	"sea-try-go/service/message/api/internal/types"
	"sea-try-go/service/message/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type MarkAdminConversationReadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMarkAdminConversationReadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MarkAdminConversationReadLogic {
	return &MarkAdminConversationReadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MarkAdminConversationReadLogic) MarkAdminConversationRead(req *types.MarkConversationReadReq) (resp *types.EmptyReq, err error) {
	started := time.Now()
	const route = "/message/v1/admin/conversations/read"
	defer func() {
		metrics.ObserveRequest(route, started, err)
	}()

	uid, err := shared.UserIDFromContext(l.ctx)
	if err != nil {
		metrics.ObserveReject(route, "admin_id_missing")
		return nil, err
	}

	_, err = l.svcCtx.MessageRpc.MarkConversationRead(l.ctx, &pb.MarkConversationReadReq{
		OperatorId:     uid,
		OperatorRole:   pb.SenderRole_ADMIN,
		ConversationId: req.ConversationId,
	})
	if err != nil {
		return nil, shared.RPCError(err)
	}

	return &types.EmptyReq{}, nil
}
