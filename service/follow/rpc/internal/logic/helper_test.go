package logic

import (
	"testing"

	pb "github.com/Sea-Go/Sea-RideTheWind/service/follow/rpc/pb"
)

// G5 测试补底：follow 域首个测试——关系请求校验与列表参数归一纯函数。

func TestFollowValidateRelationReq(t *testing.T) {
	if err := validateRelationReq(nil); err == nil {
		t.Error("nil 请求应拒绝")
	}
	if err := validateRelationReq(&pb.RelationReq{UserId: 0, TargetId: 5}); err == nil {
		t.Error("非正 userId 应拒绝")
	}
	if err := validateRelationReq(&pb.RelationReq{UserId: 5, TargetId: 5}); err == nil {
		t.Error("自关注/自拉黑应拒绝")
	}
	if err := validateRelationReq(&pb.RelationReq{UserId: 5, TargetId: 6}); err != nil {
		t.Errorf("合法请求应通过，实际 %v", err)
	}
}

func TestFollowNormalizeListReq(t *testing.T) {
	uid, offset, limit, err := normalizeListReq(20, 100, &pb.ListReq{UserId: 7, Offset: -3, Limit: 0})
	if err != nil || uid != 7 || offset != 0 || limit != 20 {
		t.Fatalf("负偏移回零/零限额回默认 失败: uid=%d offset=%d limit=%d err=%v", uid, offset, limit, err)
	}
	_, _, limit, err = normalizeListReq(20, 100, &pb.ListReq{UserId: 7, Limit: 999})
	if err != nil || limit != 100 {
		t.Fatalf("超上限应截断，实际 limit=%d err=%v", limit, err)
	}
	if _, _, _, err := normalizeListReq(20, 100, &pb.ListReq{UserId: 0}); err == nil {
		t.Error("非法 userId 应拒绝")
	}
	if _, _, _, err := normalizeListReq(20, 100, nil); err == nil {
		t.Error("nil 请求应拒绝")
	}
}
