package model

import "time"

type FavoriteFolder struct {
	Id         uint64    `gorm:"primaryKey"`
	FolderId   int64     `gorm:"column:folder_id;uniqueIndex;not null"`
	UserId     int64     `gorm:"column:user_id;not null;index;uniqueIndex:uk_user_folder_name"`
	Name       string    `gorm:"column:name;type:varchar(100);not null;uniqueIndex:uk_user_folder_name"`
	CreateTime time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time `gorm:"column:update_time;autoUpdateTime"`
}

func (FavoriteFolder) TableName() string {
	return "favorite_folder"
}

type FavoriteItem struct {
	Id         uint64    `gorm:"primaryKey"`
	FavoriteId int64     `gorm:"column:favorite_id;uniqueIndex;not null"`
	FolderId   int64     `gorm:"column:folder_id;not null;index;uniqueIndex:uk_folder_target"`
	UserId     int64     `gorm:"column:user_id;not null;index"`
	TargetId   string    `gorm:"column:target_id;type:varchar(255);not null;index;uniqueIndex:uk_folder_target"`
	TargetType string    `gorm:"column:target_type;type:varchar(50);not null;index;uniqueIndex:uk_folder_target"`
	Title      string    `gorm:"column:title;type:varchar(255)"`
	Cover      string    `gorm:"column:cover;type:varchar(500)"`
	CreateTime time.Time `gorm:"column:create_time;autoCreateTime"`
}

func (FavoriteItem) TableName() string {
	return "favorite_item"
}
