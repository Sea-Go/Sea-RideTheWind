package logic

import "testing"

// G5 测试补底：points 域首个测试——积分动作判别纯函数。

func TestPointsActionBySign(t *testing.T) {
	if got := pointsAction(10); got != "add" {
		t.Errorf("正数应为 add，实际 %q", got)
	}
	if got := pointsAction(-5); got != "dec" {
		t.Errorf("负数应为 dec，实际 %q", got)
	}
	if got := pointsAction(0); got != "add" {
		t.Errorf("零按增加处理（既有语义），实际 %q", got)
	}
}
