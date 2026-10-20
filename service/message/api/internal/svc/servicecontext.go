// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package svc

import (
	"github.com/Sea-Go/Sea-RideTheWind/service/message/api/internal/config"
	"github.com/Sea-Go/Sea-RideTheWind/service/message/rpc/messageservice"

	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config     config.Config
	MessageRpc messageservice.MessageService
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:     c,
		MessageRpc: messageservice.NewMessageService(zrpc.MustNewClient(c.MessageRpc)),
	}
}
