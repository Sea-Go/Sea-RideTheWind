// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package like

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/api/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

type GetLikeStateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetLikeStateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetLikeStateLogic {
	return &GetLikeStateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetLikeStateLogic) GetLikeState(req *types.GetLikeStateReq) (resp *types.GetLikeStateResp, err error) {
	userId, ok := l.ctx.Value("userId").(json.Number)
	if !ok {
		metrics.ApiRejectCount.WithLabelValues("/like/likeaction", "token_invalid").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, fmt.Errorf("ctx userId is not json.Number"))
		return nil, errmsg.NewErrCode(errmsg.ErrorTokenRuntime)
	}
	uid, err := userId.Int64()

	if err != nil {
		metrics.ApiRejectCount.WithLabelValues("/like/likeaction", "token_parse_error").Inc()
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, fmt.Errorf("parse userId to int64 failed: %v", err))
		return nil, errmsg.NewErrCode(errmsg.ErrorTokenRuntime)
	}

	rpcReq := &pb.GetLikeStateReq{
		TargetType: req.TargetType,
		UserId:     uid,
		TargetIds:  req.TargetIds,
	}
	rpcResp, err := l.svcCtx.LikeRpc.GetLikeState(l.ctx, rpcReq)
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			return nil, errmsg.NewErrCodeMsg(int(st.Code()), st.Message())
		}
		logger.LogBusinessErr(l.ctx, errmsg.ErrorServerCommon, err)
		return nil, errmsg.NewErrCode(errmsg.ErrorServerCommon)
	}
	return &types.GetLikeStateResp{
		States: rpcResp.States,
	}, nil
}
