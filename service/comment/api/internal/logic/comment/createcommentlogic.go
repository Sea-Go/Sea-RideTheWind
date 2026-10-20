// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package comment

import (
	"context"
	"fmt"

	"github.com/Sea-Go/Sea-RideTheWind/service/comment/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/pb"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

type CreateCommentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateCommentLogic {
	return &CreateCommentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateCommentLogic) CreateComment(req *types.CreateCommentReq) (resp *types.CreateCommentResp, err error) {
	uid, err := extractUserID(l.ctx)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, fmt.Errorf("extract userId from context failed: %w", err))
		return nil, errmsg.NewErrCode(errmsg.ErrorTokenRuntime)
	}
	rpcReq := &pb.CreateCommentReq{
		UserId:     uid,
		TargetType: req.TargetType,
		TargetId:   req.TargetId,
		RootId:     req.RootId,
		ParentId:   req.ParentId,
		Content:    req.Content,
		Meta:       req.Meta,
	}

	rpcResp, err := l.svcCtx.CommentCli.CreateComment(l.ctx, rpcReq)
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			return nil, errmsg.NewErrCodeMsg(int(st.Code()), st.Message())
		}
		logger.LogBusinessErr(l.ctx, errmsg.ErrorServerCommon, err)
		return nil, errmsg.NewErrCode(errmsg.ErrorServerCommon)
	}

	return &types.CreateCommentResp{
		Id:           rpcResp.Id,
		CreatedAt:    rpcResp.CreatedAt,
		SubjectCount: rpcResp.SubjectCount,
	}, nil
}
