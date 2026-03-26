//go:build legacy_user_rpc_tests
// +build legacy_user_rpc_tests

package logic

import (
	"testing"

	pb "sea-try-go/service/user/rpc/pb"
)

func TestLogout_Success(t *testing.T) {
	db := setupTestDB()

	svcCtx := setupTestServiceContext(db)
	ctx := newTestContext()

	logic := NewLogoutLogic(ctx, svcCtx)

	req := &pb.LogoutReq{
		Token: "some-token",
	}

	resp, err := logic.Logout(req)

	if err != nil {
		t.Fatalf("登出请求失败: %v", err)
	}

	// 当前 Logout 逻辑为空，只返回空响应
	if resp == nil {
		t.Error("响应不应为 nil")
	}

	t.Log("✅ 登出测试完成（当前逻辑为空实现）")
}
