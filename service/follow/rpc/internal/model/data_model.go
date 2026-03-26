package model

import "time"

type FollowRelation struct {
	ID         uint64    `gorm:"primaryKey"`
	UserId     int64     `gorm:"column:user_id;not null;index;uniqueIndex:uk_follow_pair"`
	TargetId   int64     `gorm:"column:target_id;not null;index;uniqueIndex:uk_follow_pair"`
	CreateTime time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time `gorm:"column:update_time;autoUpdateTime"`
}

func (FollowRelation) TableName() string {
	return "follow_relation"
}

type BlockRelation struct {
	ID         uint64    `gorm:"primaryKey"`
	UserId     int64     `gorm:"column:user_id;not null;index;uniqueIndex:uk_block_pair"`
	TargetId   int64     `gorm:"column:target_id;not null;index;uniqueIndex:uk_block_pair"`
	CreateTime time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time `gorm:"column:update_time;autoUpdateTime"`
}

func (BlockRelation) TableName() string {
	return "block_relation"
}
