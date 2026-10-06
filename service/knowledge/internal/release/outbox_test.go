package release

import (
	"fmt"
	"sync"
	"testing"
)

func eventN(n int) Event {
	e := validEvent()
	e.EventID = fmt.Sprintf("evt-release-%03d", n)
	e.Docs = []EventDoc{validDoc("source", fmt.Sprintf("11111111-1111-4111-8111-%012d", n), fmt.Sprintf("revision_%02d", n))}
	return e
}

func TestMemoryStoreAppendIdempotent(t *testing.T) {
	st := NewMemoryStore()
	e := eventN(1)
	for i := 0; i < 3; i++ {
		if err := st.Append(e); err != nil {
			t.Fatalf("第 %d 次 Append: %v", i+1, err)
		}
	}
	pending, err := st.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("幂等 Append 后 Pending 数 = %d, 期望 1", len(pending))
	}
}

func TestMemoryStorePendingKeepsInsertOrder(t *testing.T) {
	st := NewMemoryStore()
	for i := 1; i <= 3; i++ {
		if err := st.Append(eventN(i)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	pending, _ := st.Pending()
	for i, e := range pending {
		want := fmt.Sprintf("evt-release-%03d", i+1)
		if e.EventID != want {
			t.Fatalf("Pending[%d] = %q, 期望 %q", i, e.EventID, want)
		}
	}
	if err := st.MarkSent("evt-release-002"); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	pending, _ = st.Pending()
	if len(pending) != 2 || pending[0].EventID != "evt-release-001" || pending[1].EventID != "evt-release-003" {
		t.Fatalf("MarkSent 后 Pending 应跳过已发送且保序, got %v", pending)
	}
	if err := st.MarkSent("evt-release-002"); err != nil {
		t.Fatalf("重复 MarkSent 应幂等: %v", err)
	}
	if err := st.MarkSent("evt-unknown"); err == nil {
		t.Fatalf("未知 event_id 的 MarkSent 应报错")
	}
}

func TestMemoryStoreDefensiveCopies(t *testing.T) {
	st := NewMemoryStore()
	e := eventN(1)
	if err := st.Append(e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	want := e.Docs[0].DocKey
	// Append 之后调用方继续改自己的 Docs 底层数组，不能穿透到已存事件。
	e.Docs[0].DocKey = "source:00000000-0000-4000-8000-000000000000@drift"
	pending, err := st.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if got := pending[0].Docs[0].DocKey; got != want {
		t.Fatalf("调用方切片改动穿透进存储: %q", got)
	}
	// Pending 的返回值同样与存储解耦：改动返回事件不影响后续读取/投递。
	pending[0].Docs[0].RevisionID = "drift"
	again, _ := st.Pending()
	if got := again[0].Docs[0].RevisionID; got == "drift" {
		t.Fatal("Pending 返回值的改动穿透进了存储")
	}
}

func TestDispatchAllSuccess(t *testing.T) {
	st := NewMemoryStore()
	for i := 1; i <= 3; i++ {
		_ = st.Append(eventN(i))
	}
	var sent []string
	n, err := Dispatch(st, func(e Event) error {
		sent = append(sent, e.EventID)
		return nil
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if n != 3 || len(sent) != 3 {
		t.Fatalf("全量投递 n=%d sent=%v", n, sent)
	}
	if sent[0] != "evt-release-001" || sent[1] != "evt-release-002" || sent[2] != "evt-release-003" {
		t.Fatalf("投递应按 Pending 序: %v", sent)
	}
	if again, _ := st.Pending(); len(again) != 0 {
		t.Fatalf("Dispatch 后仍有 %d 条待发送", len(again))
	}
	n, err = Dispatch(st, func(e Event) error { t.Fatalf("不应再发送"); return nil })
	if err != nil || n != 0 {
		t.Fatalf("空队列二次 Dispatch: n=%d err=%v", n, err)
	}
}

func TestDispatchStopsOnFailureAndResumes(t *testing.T) {
	st := NewMemoryStore()
	for i := 1; i <= 3; i++ {
		_ = st.Append(eventN(i))
	}
	fail := map[string]bool{"evt-release-002": true}
	var sent []string
	n, err := Dispatch(st, func(e Event) error {
		if fail[e.EventID] {
			return fmt.Errorf("模拟投递失败: %s", e.EventID)
		}
		sent = append(sent, e.EventID)
		return nil
	})
	if err == nil || n != 1 {
		t.Fatalf("中途失败应返回已发数 1 与错误, got n=%d err=%v", n, err)
	}
	if len(sent) != 1 || sent[0] != "evt-release-001" {
		t.Fatalf("失败前应只发 1 条: %v", sent)
	}
	// 下次续传：从失败处继续，不重发已成功的。
	fail["evt-release-002"] = false
	n, err = Dispatch(st, func(e Event) error {
		sent = append(sent, e.EventID)
		return nil
	})
	if err != nil || n != 2 {
		t.Fatalf("续传应发剩余 2 条, got n=%d err=%v", n, err)
	}
	if len(sent) != 3 || sent[1] != "evt-release-002" || sent[2] != "evt-release-003" {
		t.Fatalf("续传顺序不符: %v", sent)
	}
}

// failMarkStore 让 MarkSent 始终失败，验证 send 成功但标记失败时
// Dispatch 返回 0（该条按未投递计，at-least-once 重发）。
type failMarkStore struct {
	*MemoryStore
}

func (f failMarkStore) MarkSent(eventID string) error {
	return fmt.Errorf("模拟 MarkSent 失败: %s", eventID)
}

func TestDispatchMarkSentFailure(t *testing.T) {
	st := failMarkStore{NewMemoryStore()}
	_ = st.Append(eventN(1))
	n, err := Dispatch(st, func(e Event) error { return nil })
	if err == nil || n != 0 {
		t.Fatalf("MarkSent 失败应返回 0 与错误, got n=%d err=%v", n, err)
	}
}

func TestMemoryStoreConcurrentAppend(t *testing.T) {
	st := NewMemoryStore()
	const workers, uniqueEach = 16, 4
	var wg sync.WaitGroup
	// 每个并发者反复追加同一共享事件（幂等竞态）+ 各自独占事件。
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			shared := eventN(0)
			for i := 0; i < 8; i++ {
				if err := st.Append(shared); err != nil {
					t.Errorf("并发 Append shared: %v", err)
					return
				}
			}
			for u := 0; u < uniqueEach; u++ {
				if err := st.Append(eventN(w*uniqueEach + u + 1)); err != nil {
					t.Errorf("并发 Append unique: %v", err)
					return
				}
			}
		}(w)
	}
	// 并发读者在写入期间持续拉 Pending。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if _, err := st.Pending(); err != nil {
				t.Errorf("并发 Pending: %v", err)
				return
			}
		}
	}()
	wg.Wait()
	pending, err := st.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	wantCount := 1 + workers*uniqueEach
	if len(pending) != wantCount {
		t.Fatalf("并发 Append 后 Pending 数 = %d, 期望 %d", len(pending), wantCount)
	}
	seen := make(map[string]bool)
	for _, e := range pending {
		if seen[e.EventID] {
			t.Fatalf("事件 %q 重复存储", e.EventID)
		}
		seen[e.EventID] = true
	}
	// 并发 MarkSent 全部后清空。
	for id := range seen {
		if err := st.MarkSent(id); err != nil {
			t.Fatalf("MarkSent %q: %v", id, err)
		}
	}
	if left, _ := st.Pending(); len(left) != 0 {
		t.Fatalf("全部 MarkSent 后仍有 %d 条", len(left))
	}
}
