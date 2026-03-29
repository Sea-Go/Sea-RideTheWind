package model

import (
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"sea-try-go/service/article/rpc/internal/config"
)

type ArticleRepo struct {
	Db *gorm.DB
}

func NewArticleRepo(c config.Config) *ArticleRepo {
	db, err := InitDB(c)
	if err != nil {
		logx.Errorf("init db error:%v", err)
		panic(err)
	}

	logx.Infof("init db success")
	return &ArticleRepo{Db: db}
}

func InitDB(c config.Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=Asia/Shanghai",
		c.Postgres.Host,
		c.Postgres.Port,
		c.Postgres.User,
		c.Postgres.Password,
		c.Postgres.Dbname,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		SkipDefaultTransaction:                   true,
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	if err := db.AutoMigrate(
		&Article{},
		&ArticleSyncOutboxEvent{},
	); err != nil {
		return nil, fmt.Errorf("failed to auto migrate models: %w", err)
	}

	// Keep the local pool conservative so the outbox poller does not exhaust
	// the shared Postgres instance.
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	return db, nil
}
