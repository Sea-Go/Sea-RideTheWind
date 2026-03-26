// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package message

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

type GetConversationMessagesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetConversationMessagesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetConversationMessagesLogic {
	return &GetConversationMessagesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetConversationMessagesLogic) GetConversationMessages(req *types.ConversationMessagesReq) (resp *types.ConversationMessagesResp, err error) {
	started := time.Now()
	const route = "/message/v1/conversations/messages"
	defer func() {
		metrics.ObserveRequest(route, started, err)
	}()

	uid, err := shared.UserIDFromContext(l.ctx)
	if err != nil {
		metrics.ObserveReject(route, "user_id_missing")
		return nil, err
	}

	rpcResp, err := l.svcCtx.MessageRpc.GetConversationMessages(l.ctx, &pb.ConversationMessageListReq{
		OperatorId:     uid,
		OperatorRole:   pb.SenderRole_USER,
		ConversationId: req.ConversationId,
		Offset:         req.Offset,
		Limit:          req.Limit,
	})
	if err != nil {
		return nil, shared.RPCError(err)
	}

	return shared.ToConversationMessagesResp(rpcResp), nil
}
