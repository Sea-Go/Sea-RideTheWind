//go:build legacy_user_rpc_tests
// +build legacy_user_rpc_tests

package logic

import (
	"testing"

	pb "sea-try-go/service/user/rpc/pb"
)

func TestUpdateUser_UpdateUsername(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	// 创建测试用户
	testUser := createTestUser(db, "updateuser", "password123", "update@example.com")

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewUpdateUserLogic(ctx, svcCtx)

	req := &pb.UpdateUserReq{
		Uid:      testUser.Uid,
		Username: "newusername",
	}

	resp, err := logic.UpdateUser(req)

	if err != nil {
		t.Fatalf("更新用户失败: %v", err)
	}

	if resp == nil {
		t.Fatal("响应不应为 nil")
	}

	// 验证更新结果
	var updatedUser TestUser
	db.Where("uid = ?", testUser.Uid).First(&updatedUser)

	if updatedUser.Username != "newusername" {
		t.Errorf("用户名未更新: 期望 %s, 实际 %s", "newusername", updatedUser.Username)
	}

	t.Log("✅ 更新用户名成功")
}

func TestUpdateUser_UpdateEmail(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	testUser := createTestUser(db, "emailuser", "password123", "old@example.com")

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewUpdateUserLogic(ctx, svcCtx)

	req := &pb.UpdateUserReq{
		Uid:   testUser.Uid,
		Email: "new@example.com",
	}

	resp, err := logic.UpdateUser(req)

	if err != nil {
		t.Fatalf("更新邮箱失败: %v", err)
	}

	if resp == nil {
		t.Fatal("响应不应为 nil")
	}

	// 验证更新结果
	var updatedUser TestUser
	db.Where("uid = ?", testUser.Uid).First(&updatedUser)

	if updatedUser.Email != "new@example.com" {
		t.Errorf("邮箱未更新: 期望 %s, 实际 %s", "new@example.com", updatedUser.Email)
	}

	t.Log("✅ 更新邮箱成功")
}

func TestUpdateUser_UpdatePassword(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	testUser := createTestUser(db, "pwduser", "oldpassword", "pwd@example.com")

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewUpdateUserLogic(ctx, svcCtx)

	req := &pb.UpdateUserReq{
		Uid:      testUser.Uid,
		Password: "newpassword",
	}

	resp, err := logic.UpdateUser(req)

	if err != nil {
		t.Fatalf("更新密码失败: %v", err)
	}

	if resp == nil {
		t.Fatal("响应不应为 nil")
	}

	// 验证新密码可以登录
	var updatedUser TestUser
	db.Where("uid = ?", testUser.Uid).First(&updatedUser)

	// 密码应该已更改（哈希值不同）
	if updatedUser.Password == testUser.Password {
		t.Error("密码应该已更新")
	}

	t.Log("✅ 更新密码成功")
}

func TestUpdateUser_DuplicateUsername(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	// 创建两个用户
	createTestUser(db, "user1", "password123", "user1@example.com")
	user2 := createTestUser(db, "user2", "password123", "user2@example.com")

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewUpdateUserLogic(ctx, svcCtx)

	// 尝试将 user2 的用户名改为 user1（已存在）
	req := &pb.UpdateUserReq{
		Uid:      user2.Uid,
		Username: "user1",
	}

	_, err := logic.UpdateUser(req)
	if err == nil {
		t.Fatal("用户名已存在时应该返回错误")
	}

	t.Log("✅ 重复用户名更新被正确拒绝")
}

func TestUpdateUser_UpdateExtraInfo(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	testUser := createTestUser(db, "extrauser", "password123", "extra@example.com")

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewUpdateUserLogic(ctx, svcCtx)

	req := &pb.UpdateUserReq{
		Uid: testUser.Uid,
		ExtraInfo: map[string]string{
			"hobby": "coding",
			"city":  "Beijing",
		},
	}

	resp, err := logic.UpdateUser(req)

	if err != nil {
		t.Fatalf("更新额外信息失败: %v", err)
	}

	if resp == nil {
		t.Fatal("响应不应为 nil")
	}

	// 验证更新结果
	var updatedUser TestUser
	db.Where("uid = ?", testUser.Uid).First(&updatedUser)

	if updatedUser.ExtraInfo["hobby"] != "coding" || updatedUser.ExtraInfo["city"] != "Beijing" {
		t.Errorf("额外信息未更新正确: 期望 %v, 实际 %v", map[string]string{"hobby": "coding", "city": "Beijing"}, updatedUser.ExtraInfo)
	}

	t.Log("✅ 更新额外信息成功")
}
