package logic

import (
	"github.com/Sea-Go/Sea-RideTheWind/service/common/paging"
	"testing"

	pb "github.com/Sea-Go/Sea-RideTheWind/service/message/rpc/pb"
)

// G5 测试补底：message 域首个测试——角色/限额归一/CJK 长度/会话参与方规范化纯函数。

func TestMessageValidRole(t *testing.T) {
	if !validRole(pb.SenderRole_USER) || !validRole(pb.SenderRole_ADMIN) {
		t.Error("USER/ADMIN 应为合法角色")
	}
	if validRole(pb.SenderRole(99)) {
		t.Error("未知角色应拒绝")
	}
}

func TestMessageNormalizeLimit(t *testing.T) {
	if got := paging.Normalize(int32(0), 20, 100); got != 20 {
		t.Errorf("limit<=0 应回退默认，实际 %d", got)
	}
	if got := paging.Normalize(int32(500), 20, 100); got != 100 {
		t.Errorf("超上限应截断，实际 %d", got)
	}
	if got := paging.Normalize(int32(50), 20, 100); got != 50 {
		t.Errorf("合法值应保留，实际 %d", got)
	}
	if got := paging.Normalize(int32(50), 20, 0); got != 50 {
		t.Errorf("maxLimit<=0 时不应截断，实际 %d", got)
	}
}

func TestMessageRuneLenCountsCJKCorrectly(t *testing.T) {
	if got := runeLen("你好sea"); got != 5 {
		t.Errorf("CJK 应按字符计数（期望 5），实际 %d", got)
	}
	if got := trimContent("  hi  "); got != "hi" {
		t.Errorf("trim 失败: %q", got)
	}
}

func TestMessageCanonicalParticipantsIsOrderIndependent(t *testing.T) {
	r1, id1, r2, id2 := canonicalParticipants(pb.SenderRole_ADMIN, 7, pb.SenderRole_USER, 3)
	r3, id3, r4, id4 := canonicalParticipants(pb.SenderRole_USER, 3, pb.SenderRole_ADMIN, 7)
	if r1 != r3 || id1 != id3 || r2 != r4 || id2 != id4 {
		t.Fatalf("参与方规范化应与入参顺序无关: (%v,%d,%v,%d) vs (%v,%d,%v,%d)", r1, id1, r2, id2, r3, id3, r4, id4)
	}
	if r1 > r2 {
		t.Fatalf("规范化后首参与方角色序应不大于后者: %v > %v", r1, r2)
	}
}
