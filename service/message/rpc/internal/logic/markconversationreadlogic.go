package logic

import (
	"context"
	"errors"
	"time"

	messagecommon "sea-try-go/service/message/common"
	"sea-try-go/service/message/rpc/internal/metrics"
	"sea-try-go/service/message/rpc/internal/model"
	"sea-try-go/service/message/rpc/internal/svc"
	"sea-try-go/service/message/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
)

type MarkConversationReadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMarkConversationReadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MarkConversationReadLogic {
	return &MarkConversationReadLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *MarkConversationReadLogic) MarkConversationRead(in *pb.MarkConversationReadReq) (*pb.BaseResp, error) {
	started := time.Now()
	var err error
	defer func() {
		metrics.ObserveRPC("MarkConversationRead", started, err)
	}()

	if in.OperatorId <= 0 || in.ConversationId <= 0 || !validRole(in.OperatorRole) {
		err = errWithBizCode(codes.InvalidArgument, messagecommon.ErrorInvalidParam)
		return nil, err
	}

	conversation, err := l.svcCtx.MessageModel.FindConversationByID(l.ctx, in.ConversationId)
	if err != nil {
		if errors.Is(err, model.ErrRecordNotFound) {
			err = errWithBizCode(codes.NotFound, messagecommon.ErrorConversationMiss)
			return nil, err
		}
		metrics.ObserveDBError("conversation", "find")
		return nil, err
	}

	if !isParticipant(conversation, in.OperatorRole, in.OperatorId) {
		err = errWithBizCode(codes.PermissionDenied, messagecommon.ErrorForbidden)
		return nil, err
	}

	if err = l.svcCtx.MessageModel.MarkConversationRead(l.ctx, in.ConversationId, int32(in.OperatorRole), in.OperatorId); err != nil {
		metrics.ObserveDBError("conversation", "mark_read")
		return nil, err
	}

	return successBaseResp(), nil
}
