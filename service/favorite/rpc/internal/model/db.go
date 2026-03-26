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
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=Asia/Shanghai",
		conf.Host,
		conf.User,
		conf.Password,
		conf.DBName,
		conf.Port,
		conf.Mode,
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalln("database connection failed")
	}
	err = db.AutoMigrate(&FavoriteFolder{}, &FavoriteItem{})
	if err != nil {
		log.Fatalln("database migration failed")
	}
	if err = migrateFavoriteTargetIDToString(db); err != nil {
		log.Fatalf("favorite_item.target_id migration failed: %v", err)
	}
	return db
}

func migrateFavoriteTargetIDToString(db *gorm.DB) error {
	var dataType string
	err := db.Raw(`
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'favorite_item'
		  AND column_name = 'target_id'
	`).Scan(&dataType).Error
	if err != nil {
		return err
	}
	if dataType == "" || dataType == "character varying" || dataType == "text" {
		return nil
	}

	return db.Exec(`
		ALTER TABLE favorite_item
		ALTER COLUMN target_id TYPE varchar(255)
		USING target_id::varchar
	`).Error
}
