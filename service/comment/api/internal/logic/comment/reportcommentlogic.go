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

type ReportCommentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewReportCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReportCommentLogic {
	return &ReportCommentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ReportCommentLogic) ReportComment(req *types.ReportCommentReq) (resp *types.ReportCommentResp, err error) {
	uid, err := extractUserID(l.ctx)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, fmt.Errorf("extract userId from context failed: %w", err))
		return nil, errmsg.NewErrCode(errmsg.ErrorTokenRuntime)
	}

	rpcReq := &pb.ReportCommentReq{
		UserId:     uid,
		CommentId:  req.CommentId,
		TargetType: req.TargetType,
		TargetId:   req.TargetId,
		Reason:     pb.ReportReason(req.Reason),
		Detail:     req.Detail,
	}

	rpcResp, err := l.svcCtx.CommentCli.ReportComment(l.ctx, rpcReq)

	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			return nil, errmsg.NewErrCodeMsg(int(st.Code()), st.Message())
		}
		logger.LogBusinessErr(l.ctx, errmsg.ErrorServerCommon, err)
		return nil, errmsg.NewErrCode(errmsg.ErrorServerCommon)
	}

	return &types.ReportCommentResp{
		Success: rpcResp.Success,
	}, nil
}
