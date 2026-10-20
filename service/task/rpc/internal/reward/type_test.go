package reward

import (
	"testing"
)

// 危险默认修复回归：旧实现 RewardID = uid+taskID，(1,9) 与 (9,1) 碰撞导致积分幂等失效。
func TestIdempotencyKeyNoCommutativeCollision(t *testing.T) {
	if idempotencyKey(1, 9) == idempotencyKey(9, 1) {
		t.Fatal("交换 uid/taskID 必须得到不同幂等键")
	}
	a := NewEventFromInts(42, 7, 5)
	b := NewEventFromInts(42, 7, 5)
	if a.RewardID != b.RewardID {
		t.Fatal("同 (uid,taskID) 幂等键必须确定")
	}
	if a.RewardID == 42+7 {
		t.Fatal("不得退回加法合成")
	}
}

func TestNewEventRejectsInvalidIDs(t *testing.T) {
	for _, bad := range []struct{ uid, task string }{
		{"abc", "7"}, {"7", "abc"}, {"", "7"}, {"7", ""},
	} {
		if ev, err := NewEvent(bad.uid, bad.task, 5); err == nil {
			t.Fatalf("NewEvent(%q,%q) 应报错，实际返回 %+v", bad.uid, bad.task, ev)
		}
	}
	ev, err := NewEvent("12", "34", 5)
	if err != nil || ev.UID != 12 || ev.TaskID != 34 {
		t.Fatalf("合法输入应正确解析: %+v err=%v", ev, err)
	}
	if ev.RewardID != idempotencyKey(12, 34) {
		t.Fatal("字符串/int 两个构造入口必须产生同一幂等键")
	}
}

// 两个构造入口一致性是 like_sink（字符串）与 article_sink（int64）共用去重语义的前提。
func TestConstructorsAgree(t *testing.T) {
	fromStr, err := NewEvent("100", "200", 5)
	if err != nil {
		t.Fatal(err)
	}
	fromInts := NewEventFromInts(100, 200, 5)
	if fromStr.RewardID != fromInts.RewardID {
		t.Fatalf("入口不一致: %d vs %d", fromStr.RewardID, fromInts.RewardID)
	}
}
