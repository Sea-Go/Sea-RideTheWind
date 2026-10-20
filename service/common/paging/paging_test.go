package paging

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct {
		limit, def, max, want int
	}{
		{0, 20, 100, 20},    // 非正回退默认
		{-5, 20, 100, 20},   // 负值同回退
		{500, 20, 100, 100}, // 超上限截断
		{50, 20, 100, 50},   // 合法保留
		{50, 20, 0, 50},     // max<=0 不设上限
		{0, 20, 0, 20},      // 无上限时回退默认仍生效
	}
	for _, c := range cases {
		if got := Normalize(c.limit, c.def, c.max); got != c.want {
			t.Errorf("Normalize(%d,%d,%d)=%d, want %d", c.limit, c.def, c.max, got, c.want)
		}
	}
}

func TestNormalizeTypes(t *testing.T) {
	if got := Normalize(int32(0), int32(20), int32(100)); got != 20 {
		t.Errorf("int32 实例化失败: %d", got)
	}
	if got := Normalize(int64(500), int64(20), int64(100)); got != 100 {
		t.Errorf("int64 实例化失败: %d", got)
	}
}

func TestNormalizeOffset(t *testing.T) {
	if got := NormalizeOffset(-3); got != 0 {
		t.Errorf("负偏移应归零: %d", got)
	}
	if got := NormalizeOffset(7); got != 7 {
		t.Errorf("非负偏移应保留: %d", got)
	}
}
