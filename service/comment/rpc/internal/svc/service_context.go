package svc

import (
	cache2 "sea-try-go/service/comment/rpc/internal/cache"
	"sea-try-go/service/comment/rpc/internal/config"
	model2 "sea-try-go/service/comment/rpc/internal/model"
	"sea-try-go/service/comment/rpc/internal/utils"
	"sea-try-go/service/article/rpc/articleservice"
	"sea-try-go/service/message/rpc/messageservice"

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
