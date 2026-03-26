//go:build legacy_user_rpc_tests
// +build legacy_user_rpc_tests

package logic

import (
	"testing"

	pb "sea-try-go/service/user/rpc/pb"
)

func TestLogin_Success(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	// 创建测试用户
	createTestUser(db, "loginuser", "correctpassword", "login@example.com")

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewLoginLogic(ctx, svcCtx)

	req := &pb.LoginReq{
		Username: "loginuser",
		Password: "correctpassword",
	}

	resp, err := logic.Login(req)

	if err != nil {
		t.Fatalf("登录请求失败: %v", err)
	}

	if resp.Status != 0 {
		t.Errorf("登录应成功(status=0), 实际 status=%d", resp.Status)
	}

	if resp.Uid == 0 {
		t.Error("登录成功应返回有效的用户UID")
	}

	t.Logf("✅ 登录成功，用户UID: %d", resp.Uid)
}

func TestLogin_UserNotFound(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewLoginLogic(ctx, svcCtx)

	req := &pb.LoginReq{
		Username: "nonexistent",
		Password: "anypassword",
	}

	resp, err := logic.Login(req)

	if err != nil {
		t.Fatalf("登录请求失败: %v", err)
	}

	if resp.Status != 1 {
		t.Errorf("用户不存在时应返回 status=1, 实际 status=%d", resp.Status)
	}

	t.Log("✅ 用户不存在被正确识别")
}

func TestLogin_WrongPassword(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	// 创建测试用户
	createTestUser(db, "wrongpwduser", "correctpassword", "wrongpwd@example.com")

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewLoginLogic(ctx, svcCtx)

	req := &pb.LoginReq{
		Username: "wrongpwduser",
		Password: "wrongpassword",
	}

	resp, err := logic.Login(req)

	if err != nil {
		t.Fatalf("登录请求失败: %v", err)
	}

	if resp.Status != 1 {
		t.Errorf("密码错误时应返回 status=1, 实际 status=%d", resp.Status)
	}

	t.Log("✅ 密码错误被正确识别")
}

func TestLogin_UserDisabled(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	// 创建被禁用的用户 (status=1)
	createTestUserWithStatus(db, "disableduser", "password123", "disabled@example.com", 1)

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewLoginLogic(ctx, svcCtx)

	req := &pb.LoginReq{
		Username: "disableduser",
		Password: "password123",
	}

	resp, err := logic.Login(req)

	if err != nil {
		t.Fatalf("登录请求失败: %v", err)
	}

	if resp.Status != 2 {
		t.Errorf("用户被禁用时应返回 status=2, 实际 status=%d", resp.Status)
	}

	t.Log("✅ 被禁用用户登录被正确拒绝")
}
