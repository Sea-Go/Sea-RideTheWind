package model

import (
	"fmt"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type DBConf struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	Mode     string
}

func InitDB(conf DBConf) *gorm.DB {
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=Asia/Shanghai",
		conf.Host,
		conf.User,
		conf.Password,
		conf.DBName,
		conf.Port,
		conf.Mode,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect postgres: %v", err)
	}

	sqlDB, err := db.DB()
	if err == nil {
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(100)
	}

	if err := db.AutoMigrate(&FollowRelation{}, &BlockRelation{}); err != nil {
		log.Fatalf("failed to migrate follow tables: %v", err)
	}

	return db
}
