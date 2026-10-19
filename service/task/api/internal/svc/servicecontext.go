// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package svc

import (
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/Sea-Go/Sea-RideTheWind/service/task/api/internal/config"
	taskpb "github.com/Sea-Go/Sea-RideTheWind/service/task/rpc/pb"
)

/*type ServiceContext struct {
	Config config.Config
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config: c,
	}
}*/

type ServiceContext struct {
	Config  config.Config
	TaskCli taskpb.TaskServiceClient // 这里的名字以你 proto 生成的为准
}

func NewServiceContext(c config.Config) *ServiceContext {
	conn := zrpc.MustNewClient(c.TaskRpc)
	return &ServiceContext{
		Config:  c,
		TaskCli: taskpb.NewTaskServiceClient(conn.Conn()),
	}
}
