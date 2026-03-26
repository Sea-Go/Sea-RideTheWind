package model

import (
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func InitDB(dsn string) *gorm.DB {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalln("数据库连接失败")
	}
	err = db.AutoMigrate(&Admin{}, &AdminInvite{}, &User{})
	if err != nil {
		log.Fatalln("数据表迁移失败")
	}
	return db
}
