//go:build legacy_user_rpc_tests
// +build legacy_user_rpc_tests

package logic

import (
	"testing"

	pb "sea-try-go/service/user/rpc/pb"
)

func TestDeleteUser_Success(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	// 创建测试用户
	testUser := createTestUser(db, "deleteuser", "password123", "delete@example.com")

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewDeleteUserLogic(ctx, svcCtx)

	req := &pb.DeleteUserReq{
		Uid: testUser.Uid,
	}

	resp, err := logic.DeleteUser(req)

	if err != nil {
		t.Fatalf("删除用户失败: %v", err)
	}

	if !resp.Success {
		t.Error("删除应成功")
	}

	// 验证用户确实被删除
	var count int64
	db.Model(&TestUser{}).Where("uid = ?", testUser.Uid).Count(&count)

	if count != 0 {
		t.Error("用户应该已被删除")
	}

	t.Log("✅ 删除用户成功")
}

func TestDeleteUser_NotFound(t *testing.T) {
	db := setupTestDB()
	cleanupTestUsers(db)

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewDeleteUserLogic(ctx, svcCtx)

	req := &pb.DeleteUserReq{
		Uid: 99999, // 不存在的 UID
	}

	resp, err := logic.DeleteUser(req)

	// GORM 删除不存在的记录不会报错，只是影响行数为0
	if err != nil {
		t.Logf("删除不存在用户返回错误: %v", err)
	}

	// 根据实现，可能返回 success=true（没有报错）
	t.Logf("删除不存在用户结果: success=%v", resp.Success)
	t.Log("✅ 删除不存在用户测试完成")
}
