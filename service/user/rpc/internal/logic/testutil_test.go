//go:build legacy_user_rpc_tests
// +build legacy_user_rpc_tests

package logic

import (
	"context"
	"fmt"
	"log"
	"sea-try-go/service/common/cryptx"
	"sea-try-go/service/common/snowflake"
	"sea-try-go/service/user/rpc/internal/config"
	"sea-try-go/service/user/rpc/internal/model"
	"sea-try-go/service/user/rpc/internal/svc"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestUser 测试用的用户模型，使用 users 表
type TestUser struct {
	Id         uint64            `gorm:"primaryKey"`
	Uid        int64             `gorm:"column:uid;uniqueIndex;not null"`
	Username   string            `gorm:"column:username;unique"`
	Password   string            `gorm:"column:password"`
	Email      string            `gorm:"column:email;unique"`
	Status     int64             `gorm:"column:status;default:0"`
	Score      int32             `gorm:"column:score"`
	ExtraInfo  map[string]string `gorm:"column:extra_info;serializer:json"`
	CreateTime time.Time         `gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time         `gorm:"column:update_time;autoUpdateTime"`
}

func (TestUser) TableName() string {
	return "users"
}

// 测试数据库配置
const (
	testDBHost     = "127.0.0.1"
	testDBPort     = "35432"
	testDBUser     = "admin"
	testDBPassword = "Sea-TryGo"
	testDBName     = "test_db"
)

// setupTestDB 创建测试用的数据库连接
func setupTestDB() *gorm.DB {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		testDBHost, testDBUser, testDBPassword, testDBName, testDBPort)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("连接测试数据库失败: %v", err)
	}

	// 自动迁移，创建 users 表
	err = db.AutoMigrate(&TestUser{})
	if err != nil {
		log.Fatalf("自动迁移失败: %v", err)
	}

	return db
}

// setupTestServiceContext 创建测试用的 ServiceContext
func setupTestServiceContext(db *gorm.DB) *svc.ServiceContext {
	return &svc.ServiceContext{
		Config:    config.Config{},
		UserModel: model.NewUserModel(db),
	}
}

// cleanupTestUsers 清空 users 表
func cleanupTestUsers(db *gorm.DB) {
	db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE")
}

// createTestUser 创建测试用户，返回创建的用户
func createTestUser(db *gorm.DB, username, password, email string) *TestUser {
	hashedPassword, err := cryptx.PasswordEncrypt(password)
	if err != nil {
		log.Fatalf("密码加密失败: %v", err)
	}

	uid, err := snowflake.GetID()
	if err != nil {
		log.Fatalf("生成UID失败: %v", err)
	}

	user := &TestUser{
		Uid:       uid,
		Username:  username,
		Password:  hashedPassword,
		Email:     email,
		Status:    0,
		Score:     0,
		ExtraInfo: map[string]string{},
	}

	if err := db.Create(user).Error; err != nil {
		log.Fatalf("创建测试用户失败: %v", err)
	}

	return user
}

// createTestUserWithStatus 创建指定状态的测试用户
func createTestUserWithStatus(db *gorm.DB, username, password, email string, status int64) *TestUser {
	hashedPassword, err := cryptx.PasswordEncrypt(password)
	if err != nil {
		log.Fatalf("密码加密失败: %v", err)
	}

	uid, err := snowflake.GetID()
	if err != nil {
		log.Fatalf("生成UID失败: %v", err)
	}

	user := &TestUser{
		Uid:       uid,
		Username:  username,
		Password:  hashedPassword,
		Email:     email,
		Status:    status,
		Score:     0,
		ExtraInfo: map[string]string{},
	}

	if err := db.Create(user).Error; err != nil {
		log.Fatalf("创建测试用户失败: %v", err)
	}

	return user
}

// newTestContext 创建测试用的 context
func newTestContext() context.Context {
	return context.Background()
}
