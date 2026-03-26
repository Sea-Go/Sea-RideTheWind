// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package task

import (
	"context"
	taskpb "sea-try-go/service/task/rpc/pb"

	"sea-try-go/service/task/api/internal/svc"
	"sea-try-go/service/task/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetTaskLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTaskLogic {
	return &GetTaskLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetTaskLogic) GetTask(req *types.GetTaskReq) (resp *types.GetTaskResp, err error) {
	uid, err := req.Userid.Int64()
	if err != nil {
		return nil, err
	}

	rpcResp, err := l.svcCtx.TaskCli.GetTask(l.ctx, &taskpb.GetTaskReq{
		UserId: uid,
	})
	if err != nil {
		return nil, err
	}
	// RPC 响应 -> API 响应
	out := make([]types.TaskInfo, 0, len(rpcResp.Task))
	for _, t := range rpcResp.Task {
		out = append(out, types.TaskInfo{
			Name:                t.Name,
			Desc:                t.Desc,
			Task_id:             t.TaskId,
			Completion_progress: t.CompletionProgress,
			Required_progress:   t.RequiredProgress,
		})
	}
	return &types.GetTaskResp{
		Tasks: out,
	}, nil
}
