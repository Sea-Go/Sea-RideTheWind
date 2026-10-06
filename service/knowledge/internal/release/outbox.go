package release

import (
	"fmt"
	"sync"
)

// Store 是 Outbox 的存储契约（C-1 唯一写者投递侧）。
// 实现必须：Append 幂等（同 event_id 重复 Append 返回 nil 不重复存）、
// Pending 返回未发送事件且顺序稳定（按 Append 先后）。
type Store interface {
	Append(Event) error
	Pending() ([]Event, error)
	MarkSent(eventID string) error
}

// MemoryStore 是 Store 的内存实现：插入序 + 已发送集合。并发安全。
// 仅供测试与进程内演示；生产实现落在 DB（本包不依赖）。
type MemoryStore struct {
	mu     sync.Mutex
	order  []string
	events map[string]Event
	sent   map[string]bool
}

// cloneEvent 深拷贝事件的 Docs 切片:事件是不可变事实,存储与返回都不与
// 调用方共享底层数组,外部改动无法穿透到已存事件。
func cloneEvent(e Event) Event {
	if e.Docs != nil {
		e.Docs = append([]EventDoc(nil), e.Docs...)
	}
	return e
}

// NewMemoryStore 返回空内存 Outbox。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		events: make(map[string]Event),
		sent:   make(map[string]bool),
	}
}

// Append 幂等写入：event_id 已存在则直接返回 nil，不覆盖不重复存。
func (s *MemoryStore) Append(e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.events[e.EventID]; ok {
		return nil
	}
	s.events[e.EventID] = cloneEvent(e)
	s.order = append(s.order, e.EventID)
	return nil
}

// Pending 按插入序返回尚未 MarkSent 的事件。
func (s *MemoryStore) Pending() ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := make([]Event, 0, len(s.order))
	for _, id := range s.order {
		if !s.sent[id] {
			pending = append(pending, cloneEvent(s.events[id]))
		}
	}
	return pending, nil
}

// MarkSent 标记已发送；重复标记幂等返回 nil，未知 eventID 报错。
func (s *MemoryStore) MarkSent(eventID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.events[eventID]; !ok {
		return fmt.Errorf("release: MarkSent 未知 event_id %q", eventID)
	}
	s.sent[eventID] = true
	return nil
}

// Dispatch 按 Pending 顺序逐个发送：发送成功即 MarkSent，发送失败立即停止，
// 返回已完整投递（发送+标记）的条数与该错误——未发出的下次 Dispatch 续传，
// 语义为 at-least-once（MarkSent 失败的那条会被重发）。send 为 nil 视为
// 每次投递都失败。
func Dispatch(st Store, send func(Event) error) (int, error) {
	pending, err := st.Pending()
	if err != nil {
		return 0, fmt.Errorf("release: 读取待发送事件: %w", err)
	}
	sent := 0
	for _, e := range pending {
		if send == nil {
			return sent, fmt.Errorf("release: send 函数为 nil")
		}
		if err := send(e); err != nil {
			return sent, err
		}
		if err := st.MarkSent(e.EventID); err != nil {
			return sent, fmt.Errorf("release: 标记已发送 %q: %w", e.EventID, err)
		}
		sent++
	}
	return sent, nil
}
