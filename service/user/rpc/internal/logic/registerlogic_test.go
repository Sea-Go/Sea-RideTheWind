//go:build legacy_user_rpc_tests
// +build legacy_user_rpc_tests

package logic

import (
	"testing"

	"sea-try-go/service/user/rpc/internal/model"
	pb "sea-try-go/service/user/rpc/pb"
)

func TestRegister_Success(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewRegisterLogic(ctx, svcCtx)

	req := &pb.CreateUserReq{
		Username:  "testuser",
		Password:  "password123",
		Email:     "test@example.com",
		ExtraInfo: map[string]string{"key": "value"},
	}

	resp, err := logic.Register(req)

	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	if resp.Uid == 0 {
		t.Error("注册成功应返回有效的用户UID")
	}

	// 验证用户确实创建成功
	var user model.User
	if err := db.Where("username = ?", "testuser").First(&user).Error; err != nil {
		t.Fatalf("未能找到创建的用户: %v", err)
	}

	// 验证各字段
	if user.Username != "testuser" {
		t.Errorf("用户名不匹配: 期望 %s, 实际 %s", "testuser", user.Username)
	}

	if user.Email != "test@example.com" {
		t.Errorf("邮箱不匹配: 期望 %s, 实际 %s", "test@example.com", user.Email)
	}

	if user.Uid != resp.Uid {
		t.Errorf("UID不匹配: 期望 %d, 实际 %d", resp.Uid, user.Uid)
	}

	if user.Status != 0 {
		t.Errorf("状态应为0: 实际 %d", user.Status)
	}

	if user.Score != 0 {
		t.Errorf("积分应为0: 实际 %d", user.Score)
	}

	t.Logf("✅ 注册成功，用户UID: %d", resp.Uid)
}

func TestRegister_DuplicateUsername(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	// 先创建一个用户
	createTestUser(db, "existinguser", "password123", "existing@example.com")

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewRegisterLogic(ctx, svcCtx)

	// 尝试用相同用户名注册
	req := &pb.CreateUserReq{
		Username:  "existinguser",
		Password:  "newpassword",
		Email:     "new@example.com",
		ExtraInfo: map[string]string{},
	}

	_, err := logic.Register(req)

	if err == nil {
		t.Fatal("用户名已存在时应该返回错误")
	}

	if err.Error() != "用户名已存在" {
		t.Errorf("错误信息不匹配: 期望 '用户名已存在', 实际 '%s'", err.Error())
	}

	t.Log("✅ 重复用户名注册被正确拒绝")
}
