package svc

import (
	"github.com/Sea-Go/Sea-RideTheWind/service/article/rpc/articleservice"
	cache2 "github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/cache"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/config"
	model2 "github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/utils"
	"github.com/Sea-Go/Sea-RideTheWind/service/message/rpc/messageservice"

	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config          config.Config
	CommentModel    *model2.CommentModel
	CommentCache    *cache2.CommentCache
	KqPusherClient  *kq.Pusher
	SensitiveFilter *utils.SensitiveFilter
	MessageRpc      messageservice.MessageService
	ArticleRpc      articleservice.ArticleService
}

func NewServiceContext(c config.Config) *ServiceContext {
	db := model2.InitDB(c.DB.DataSource)
	rdb := cache2.InitRedis(c.BizRedis.Host)
	pusher := kq.NewPusher(c.KqPusherConf.Brokers, c.KqPusherConf.Topic)
	blackwords := []string{"傻逼", "我草"}
	return &ServiceContext{
		Config:          c,
		CommentModel:    model2.NewCommentModel(db),
		CommentCache:    cache2.NewCommentCache(rdb),
		KqPusherClient:  pusher,
		SensitiveFilter: utils.NewSensitiveFilter(blackwords),
		MessageRpc:      messageservice.NewMessageService(zrpc.MustNewClient(c.MessageRpc)),
		ArticleRpc:      articleservice.NewArticleService(zrpc.MustNewClient(c.ArticleRpc)),
	}
}
