// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package like

import (
	"context"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

type GetTargetLikerListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetTargetLikerListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTargetLikerListLogic {
	return &GetTargetLikerListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetTargetLikerListLogic) GetTargetLikerList(req *types.GetTargetLikerListReq) (resp *types.GetTargetLikerListResp, err error) {
	rpcReq := &pb.GetTargetLikerListReq{
		TargetType: req.TargetType,
		TargetId:   req.TargetId,
		Cursor:     req.Cursor,
		PageSize:   req.PageSize,
	}
	rpcResp, err := l.svcCtx.LikeRpc.GetTargetLikerList(l.ctx, rpcReq)
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			return nil, errmsg.NewErrCodeMsg(int(st.Code()), st.Message())
		}
		logger.LogBusinessErr(l.ctx, errmsg.ErrorServerCommon, err)
		return nil, errmsg.NewErrCode(errmsg.ErrorServerCommon)
	}
	list := make([]types.LikeItem, 0, len(rpcResp.List))
	for _, v := range rpcResp.List {
		list = append(list, types.LikeItem{
			UserId:    v.UserId,
			Timestamp: v.Timestamp,
		})
	}
	return &types.GetTargetLikerListResp{
		List:       list,
		IsEnd:      rpcResp.IsEnd,
		NextCursor: rpcResp.NextCursor,
	}, nil
}
