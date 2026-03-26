// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package comment

import (
	"context"
	"fmt"

	"sea-try-go/service/comment/api/internal/svc"
	"sea-try-go/service/comment/api/internal/types"
	"sea-try-go/service/comment/common/errmsg"
	"sea-try-go/service/comment/rpc/pb"
	"sea-try-go/service/common/logger"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

type UpdateSubjectStateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateSubjectStateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSubjectStateLogic {
	return &UpdateSubjectStateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateSubjectStateLogic) UpdateSubjectState(req *types.UpdateSubjectStateReq) (resp *types.UpdateSubjectStateResp, err error) {
	uid, err := extractUserID(l.ctx)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, fmt.Errorf("extract userId from context failed: %w", err))
		return nil, errmsg.NewErrCode(errmsg.ErrorTokenRuntime)
	}
	rpcReq := &pb.UpdateSubjectStateReq{
		UserId:     uid,
		TargetType: req.TargetType,
		TargetId:   req.TargetId,
		State:      pb.SubjectState(req.State),
	}

	rpcResp, err := l.svcCtx.CommentCli.UpdateSubjectState(l.ctx, rpcReq)
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			return nil, errmsg.NewErrCodeMsg(int(st.Code()), st.Message())
		}
		logger.LogBusinessErr(l.ctx, errmsg.ErrorServerCommon, err)
		return nil, errmsg.NewErrCode(errmsg.ErrorServerCommon)
	}

	return &types.UpdateSubjectStateResp{
		Success: rpcResp.Success,
	}, nil
}
