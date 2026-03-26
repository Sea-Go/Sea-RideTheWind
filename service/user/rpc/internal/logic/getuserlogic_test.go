//go:build legacy_user_rpc_tests
// +build legacy_user_rpc_tests

package logic

import (
	"testing"

	pb "sea-try-go/service/user/rpc/pb"
)

func TestGetUser_Success(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	// 创建测试用户
	testUser := createTestUser(db, "getuser", "password123", "getuser@example.com")

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewGetUserLogic(ctx, svcCtx)

	req := &pb.GetUserReq{
		Uid: testUser.Uid,
	}

	resp, err := logic.GetUser(req)

	if err != nil {
		t.Fatalf("获取用户请求失败: %v", err)
	}

	if !resp.Found {
		t.Error("应该找到用户")
	}

	if resp.User == nil {
		t.Fatal("User 不应为 nil")
	}

	if resp.User.Username != "getuser" {
		t.Errorf("用户名不匹配: 期望 %s, 实际 %s", "getuser", resp.User.Username)
	}

	if resp.User.Email != "getuser@example.com" {
		t.Errorf("邮箱不匹配: 期望 %s, 实际 %s", "getuser@example.com", resp.User.Email)
	}

	t.Logf("✅ 获取用户成功，用户名: %s", resp.User.Username)
}

func TestGetUser_NotFound(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewGetUserLogic(ctx, svcCtx)

	req := &pb.GetUserReq{
		Uid: 99999, // 不存在的 UID
	}

	resp, err := logic.GetUser(req)

	// 用户不存在时，应返回 Found=false 且 err=nil
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}

	if resp == nil {
		t.Fatal("响应不应为 nil")
	}

	if resp.Found {
		t.Error("用户不存在时 Found 应为 false")
	}

	t.Log("✅ 用户不存在被正确识别")
}
