package logic

import (
	"context"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/pb"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetUserListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserListLogic {
	return &GetUserListLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetUserListLogic) GetUserList(in *pb.GetUserListReq) (*pb.GetUserListResp, error) {

	users, total, err := l.svcCtx.AdminModel.FindUserListByKeyword(l.ctx, in.Page, in.PageSize, in.Keyword)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, err)
		return nil, status.Error(codes.Internal, "DB查询失败")
	}

	list := make([]*pb.UserInfo, 0)

	for _, user := range users {
		list = append(list, &pb.UserInfo{
			Uid:       user.Uid,
			Username:  user.Username,
			Email:     user.Email,
			Status:    uint64(user.Status),
			ExtraInfo: user.ExtraInfo,
		})
	}
	logger.LogInfo(l.ctx, fmt.Sprintf("search users success,keyword : %s", in.Keyword))
	return &pb.GetUserListResp{
		List:  list,
		Total: total,
	}, nil
}
