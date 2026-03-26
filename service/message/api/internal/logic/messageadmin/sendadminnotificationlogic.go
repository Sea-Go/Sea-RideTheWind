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
	messagecommon "sea-try-go/service/message/common"
	"sea-try-go/service/message/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type SendAdminNotificationLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSendAdminNotificationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendAdminNotificationLogic {
	return &SendAdminNotificationLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SendAdminNotificationLogic) SendAdminNotification(req *types.AdminSendNotificationReq) (resp *types.AdminSendNotificationResp, err error) {
	started := time.Now()
	const route = "/message/v1/admin/notifications/send"
	defer func() {
		metrics.ObserveRequest(route, started, err)
	}()

	uid, err := shared.UserIDFromContext(l.ctx)
	if err != nil {
		metrics.ObserveReject(route, "admin_id_missing")
		return nil, err
	}
	if !req.Broadcast && req.TargetId <= 0 {
		metrics.ObserveReject(route, "target_missing")
		return nil, shared.RPCError(messagecommon.NewErrCode(messagecommon.ErrorInvalidParam))
	}

	rpcResp, err := l.svcCtx.MessageRpc.SendNotification(l.ctx, &pb.SendNotificationReq{
		RecipientIds: recipientIDs(req),
		Broadcast:    req.Broadcast,
		SenderId:     uid,
		SenderRole:   pb.SenderRole_ADMIN,
		Kind:         shared.AdminNotificationKind(req.Broadcast, req.Kind),
		Title:        req.Title,
		Content:      req.Content,
		Extra:        req.Extra,
	})
	if err != nil {
		return nil, shared.RPCError(err)
	}

	return &types.AdminSendNotificationResp{Affected: rpcResp.Affected}, nil
}

func recipientIDs(req *types.AdminSendNotificationReq) []int64 {
	if req.Broadcast {
		return nil
	}
	return []int64{req.TargetId}
}
