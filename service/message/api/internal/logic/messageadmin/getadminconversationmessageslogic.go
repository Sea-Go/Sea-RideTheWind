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

type GetAdminConversationMessagesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetAdminConversationMessagesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAdminConversationMessagesLogic {
	return &GetAdminConversationMessagesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAdminConversationMessagesLogic) GetAdminConversationMessages(req *types.ConversationMessagesReq) (resp *types.ConversationMessagesResp, err error) {
	started := time.Now()
	const route = "/message/v1/admin/conversations/messages"
	defer func() {
		metrics.ObserveRequest(route, started, err)
	}()

	uid, err := shared.UserIDFromContext(l.ctx)
	if err != nil {
		metrics.ObserveReject(route, "admin_id_missing")
		return nil, err
	}

	rpcResp, err := l.svcCtx.MessageRpc.GetConversationMessages(l.ctx, &pb.ConversationMessageListReq{
		OperatorId:     uid,
		OperatorRole:   pb.SenderRole_ADMIN,
		ConversationId: req.ConversationId,
		Offset:         req.Offset,
		Limit:          req.Limit,
	})
	if err != nil {
		return nil, shared.RPCError(err)
	}

	return shared.ToConversationMessagesResp(rpcResp), nil
}
