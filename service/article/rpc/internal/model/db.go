package model

import (
	"fmt"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"sea-try-go/service/article/rpc/internal/config"
	"time"
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
	return &ArticleRepo{
		Db: db,
	}
}

func InitDB(c config.Config) (db *gorm.DB, err error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=Asia/Shanghai",
		c.Postgres.Host,
		c.Postgres.Port,
		c.Postgres.User,
		c.Postgres.Password,
		c.Postgres.Dbname,
	)
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		SkipDefaultTransaction:                   true,
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true, //禁用复数表名
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	// 获取底层 *sql.DB 以设置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// 自动迁移模型
	err = db.AutoMigrate(
		&Article{},
		&ArticleSyncOutboxEvent{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to auto migrate models: %w", err)
	}

	// 设置连接池
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(10 * time.Second)

	return db, nil
}
