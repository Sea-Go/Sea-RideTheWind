package logic

import "testing"

// G5 测试补底：task 域首个测试——进度缓存打包协议与解析纯函数。

func TestTaskProgressPackUnpackRoundtrip(t *testing.T) {
	cases := []struct {
		status   string
		progress int64
		target   int64
	}{
		{"running", 0, 100},
		{"done", 100, 100},
		{"pending", 5, 999999},
		{"failed", 0, 0},
	}
	for _, c := range cases {
		packed := packProgress(c.status, c.progress, c.target)
		gotStatus, gotProgress, gotTarget := unpackProgress(packed)
		if gotStatus != c.status || gotProgress != c.progress || gotTarget != c.target {
			t.Errorf("roundtrip(%v) = (%q,%d,%d), want (%q,%d,%d)", packed, gotStatus, gotProgress, gotTarget, c.status, c.progress, c.target)
		}
	}
}

func TestTaskBuildUserProgressKey(t *testing.T) {
	if got := buildUserProgressKey(42); got == "" || got == buildUserProgressKey(43) {
		t.Fatalf("key 应非空且按用户区分: %q", got)
	}
}

func TestTaskParseInt64Default(t *testing.T) {
	if got := parseInt64Default("123", 7); got != 123 {
		t.Errorf("合法数字应解析: %d", got)
	}
	for _, bad := range []string{"", "abc", "12x"} {
		if got := parseInt64Default(bad, 7); got != 7 {
			t.Errorf("parseInt64Default(%q) 应回退默认 7，实际 %d", bad, got)
		}
	}
}
