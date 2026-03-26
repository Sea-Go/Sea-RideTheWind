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

type SendAdminChatMessageLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSendAdminChatMessageLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendAdminChatMessageLogic {
	return &SendAdminChatMessageLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SendAdminChatMessageLogic) SendAdminChatMessage(req *types.AdminSendChatReq) (resp *types.SendChatResp, err error) {
	started := time.Now()
	const route = "/message/v1/admin/conversations/send"
	defer func() {
		metrics.ObserveRequest(route, started, err)
	}()

	uid, err := shared.UserIDFromContext(l.ctx)
	if err != nil {
		metrics.ObserveReject(route, "admin_id_missing")
		return nil, err
	}

	rpcResp, err := l.svcCtx.MessageRpc.SendChatMessage(l.ctx, &pb.SendChatMessageReq{
		SenderId:       uid,
		SenderRole:     pb.SenderRole_ADMIN,
		RecipientId:    req.RecipientId,
		RecipientRole:  pb.SenderRole_USER,
		ConversationId: req.ConversationId,
		Content:        req.Content,
	})
	if err != nil {
		return nil, shared.RPCError(err)
	}

	return shared.ToSendChatResp(rpcResp), nil
}
