package svc

import (
	"context"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/snowflake"
	"sea-try-go/service/user/admin/rpc/internal/config"
	"sea-try-go/service/user/admin/rpc/internal/model"
	"sea-try-go/service/user/common/cryptx"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	defaultAdminUsername = "admin"
	defaultAdminPassword = "admin"
)

type ServiceContext struct {
	Config     config.Config
	AdminModel *model.AdminModel
	BizRedis   *redis.Redis
}

func NewServiceContext(c config.Config) *ServiceContext {
	db := model.InitDB(c.DataSource)
	svcCtx := &ServiceContext{
		Config:     c,
		AdminModel: model.NewAdminModel(db),
		BizRedis:   redis.MustNewRedis(c.BizRedis),
	}
	logx.Must(svcCtx.ensureDefaultAdmin(context.Background()))
	return svcCtx
}

func (s *ServiceContext) ensureDefaultAdmin(ctx context.Context) error {
	_, err := s.AdminModel.FindOneAdminByUsername(ctx, defaultAdminUsername)
	if err == nil {
		return nil
	}
	if err != model.ErrorNotFound {
		return err
	}

	password, err := cryptx.PasswordEncrypt(defaultAdminPassword)
	if err != nil {
		return err
	}

	uid, err := snowflake.GetID()
	if err != nil {
		return err
	}

	admin := &model.Admin{
		Uid:      uid,
		Username: defaultAdminUsername,
		Password: password,
	}
	if err = s.AdminModel.InsertOneAdmin(ctx, admin); err != nil {
		if model.IsUniqueViolation(err) {
			_, lookupErr := s.AdminModel.FindOneAdminByUsername(ctx, defaultAdminUsername)
			if lookupErr == nil {
				return nil
			}
		}
		return err
	}

	logger.LogInfo(ctx, "default admin account bootstrapped")
	return nil
}
