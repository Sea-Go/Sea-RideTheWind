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

type GetLikeCountLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetLikeCountLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetLikeCountLogic {
	return &GetLikeCountLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetLikeCountLogic) GetLikeCount(req *types.GetLikeCountReq) (resp *types.GetLikeCountResp, err error) {
	rpcReq := &pb.GetLikeCountReq{
		TargetType: req.TargetType,
		TargetIds:  req.TargetIds,
	}

	rpcResp, err := l.svcCtx.LikeRpc.GetLikeCount(l.ctx, rpcReq)
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			return nil, errmsg.NewErrCodeMsg(int(st.Code()), st.Message())
		}
		logger.LogBusinessErr(l.ctx, errmsg.ErrorServerCommon, err)
		return nil, errmsg.NewErrCode(errmsg.ErrorServerCommon)
	}
	counts := make(map[string]types.LikeCountItem, len(rpcResp.Counts))
	for k, v := range rpcResp.Counts {
		counts[k] = types.LikeCountItem{
			LikeCount:    v.LikeCount,
			DislikeCount: v.DislikeCount,
		}
	}
	return &types.GetLikeCountResp{
		Counts: counts,
	}, nil
}
