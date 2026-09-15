package svc

import (
	"sea-try-go/service/article/rpc/articleservice"
	"sea-try-go/service/favorite/rpc/internal/config"
	"sea-try-go/service/favorite/rpc/internal/model"
	"sea-try-go/service/user/user/rpc/userservice"

	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config        config.Config
	FavoriteModel *model.FavoriteModel
	UserRpc       userservice.UserService
	ArticleRpc    articleservice.ArticleService
}

func NewServiceContext(c config.Config) *ServiceContext {
	dbConfig := model.DBConf{
		Host:     c.Postgres.Host,
		Port:     c.Postgres.Port,
		User:     c.Postgres.User,
		Password: c.Postgres.Password,
		DBName:   c.Postgres.DBName,
		Mode:     c.Postgres.Mode,
	}
	db := model.InitDB(dbConfig)
	var factOptions []model.FavoriteModelOption
	if c.SubjectRefV2Facts {
		factOptions = append(factOptions, model.WithSubjectRefV2Facts())
	}

	return &ServiceContext{
		Config:        c,
		FavoriteModel: model.NewFavoriteModel(db, factOptions...),
		UserRpc:       userservice.NewUserService(zrpc.MustNewClient(c.UserRpc)),
		ArticleRpc:    articleservice.NewArticleService(zrpc.MustNewClient(c.ArticleRpc)),
	}
}
