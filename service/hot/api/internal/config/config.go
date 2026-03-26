// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package config

import (
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	rest.RestConf
	HotRpcConf     zrpc.RpcClientConf
	ArticleRpcConf zrpc.RpcClientConf
}
