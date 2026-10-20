package release

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

var publishAt = time.Date(2026, 10, 6, 4, 0, 0, 0, time.UTC)

func fakeSHA(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func validDoc(prefix, uuid, rev string) EventDoc {
	return EventDoc{
		DocKey:        prefix + ":" + uuid + "@" + rev,
		RevisionID:    rev,
		S3Ref:         "s3://sea-docs/" + prefix + "-" + uuid + ".md",
		StructureRef:  "structure/" + uuid + ".tree",
		ContentSHA256: fakeSHA(prefix + uuid + rev),
		CharLen:       128,
	}
}

func validEvent() Event {
	return Event{
		EventID:     "evt-release-001",
		ModuleID:    "module_h04",
		ReleaseID:   "rel-20261006-001",
		PublishedAt: publishAt,
		Docs: []EventDoc{
			validDoc("page", "22222222-2222-4222-8222-222222222222", "revision_page_v3"),
			validDoc("source", "11111111-1111-4111-8111-111111111111", "revision_book_a_v1"),
			validDoc("summary", "33333333-3333-4333-8333-333333333333", "revision_sum_v2"),
		},
	}
}

func TestValidateAccepts(t *testing.T) {
	if err := validEvent().Validate(); err != nil {
		t.Fatalf("Validate 合法事件: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Event)
		want   string
	}{
		{"event_id 为空", func(e *Event) { e.EventID = "" }, "event_id"},
		{"module_id 为空", func(e *Event) { e.ModuleID = "" }, "module_id"},
		{"release_id 为空", func(e *Event) { e.ReleaseID = "" }, "release_id"},
		{"published_at 零值", func(e *Event) { e.PublishedAt = time.Time{} }, "published_at"},
		{"docs 为空", func(e *Event) { e.Docs = nil }, "docs"},
		{"doc_key 未知前缀", func(e *Event) { e.Docs[0].DocKey = "wiki:22222222-2222-4222-8222-222222222222@rev1" }, "doc_key"},
		{"doc_key 缺 @rev", func(e *Event) { e.Docs[0].DocKey = "page:22222222-2222-4222-8222-222222222222" }, "doc_key"},
		{"doc_key uuid 非法", func(e *Event) { e.Docs[0].DocKey = "page:not-a-uuid@rev1" }, "doc_key"},
		{"doc_key rev 为空", func(e *Event) { e.Docs[0].DocKey = "page:22222222-2222-4222-8222-222222222222@" }, "doc_key"},
		{"revision_id 为空", func(e *Event) { e.Docs[1].RevisionID = "" }, "revision_id"},
		{"s3_ref 为空", func(e *Event) { e.Docs[1].S3Ref = "" }, "s3_ref"},
		{"structure_ref 为空", func(e *Event) { e.Docs[1].StructureRef = "" }, "structure_ref"},
		{"sha256 大写", func(e *Event) { e.Docs[2].ContentSHA256 = strings.ToUpper(fakeSHA("x")) }, "content_sha256"},
		{"sha256 过短", func(e *Event) { e.Docs[2].ContentSHA256 = "abc123" }, "content_sha256"},
		{"sha256 非 hex", func(e *Event) { e.Docs[2].ContentSHA256 = strings.Repeat("z", 64) }, "content_sha256"},
		{"char_len 负数", func(e *Event) { e.Docs[0].CharLen = -1 }, "char_len"},
	}
	for _, tc := range cases {
		e := validEvent()
		tc.mutate(&e)
		err := e.Validate()
		if err == nil {
			t.Fatalf("%s: 期望被拒绝", tc.name)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: 错误 %q 未提及 %q", tc.name, err, tc.want)
		}
	}
}

func TestCanonicalJSONDeterministic(t *testing.T) {
	e := validEvent()
	first := CanonicalJSON(e)
	for i := 0; i < 2; i++ {
		if again := CanonicalJSON(e); string(again) != string(first) {
			t.Fatalf("第 %d 次序列化不一致:\n%s\n%s", i+2, first, again)
		}
	}
	// 拷贝后的同一事件也产出相同字节（值语义，不受调用间状态影响）。
	e2 := e
	if string(CanonicalJSON(e2)) != string(first) {
		t.Fatalf("拷贝事件序列化不一致")
	}
}

func TestCanonicalJSONFieldOrderSnapshot(t *testing.T) {
	e := Event{
		EventID:     "evt-release-001",
		ModuleID:    "module_h04",
		ReleaseID:   "rel-20261006-001",
		PublishedAt: publishAt,
		Docs: []EventDoc{
			{
				DocKey:        "page:22222222-2222-4222-8222-222222222222@revision_page_v3",
				RevisionID:    "revision_page_v3",
				S3Ref:         "s3://sea-docs/page-222.md",
				StructureRef:  "structure/222.tree",
				ContentSHA256: fakeSHA("page-222"),
				CharLen:       1024,
			},
			{
				DocKey:        "source:11111111-1111-4111-8111-111111111111@revision_book_a_v1",
				RevisionID:    "revision_book_a_v1",
				S3Ref:         "s3://sea-docs/book-a.md",
				StructureRef:  "structure/111.tree",
				ContentSHA256: fakeSHA("book-a"),
				CharLen:       4096,
			},
		},
	}
	want := `{"event_id":"evt-release-001","module_id":"module_h04","release_id":"rel-20261006-001","published_at":"2026-10-06T04:00:00Z","docs":[{"doc_key":"page:22222222-2222-4222-8222-222222222222@revision_page_v3","revision_id":"revision_page_v3","s3_ref":"s3://sea-docs/page-222.md","structure_ref":"structure/222.tree","content_sha256":"` +
		fakeSHA("page-222") +
		`","char_len":1024},{"doc_key":"source:11111111-1111-4111-8111-111111111111@revision_book_a_v1","revision_id":"revision_book_a_v1","s3_ref":"s3://sea-docs/book-a.md","structure_ref":"structure/111.tree","content_sha256":"` +
		fakeSHA("book-a") +
		`","char_len":4096}]}`
	if got := string(CanonicalJSON(e)); got != want {
		t.Fatalf("规范 JSON 字段序/内容不符:\n got: %s\nwant: %s", got, want)
	}
}

func TestCanonicalJSONNormalizesTimeZone(t *testing.T) {
	utc := validEvent()
	plus8 := validEvent()
	plus8.PublishedAt = publishAt.In(time.FixedZone("CST", 8*3600))
	if string(CanonicalJSON(utc)) != string(CanonicalJSON(plus8)) {
		t.Fatalf("同一时刻不同时区表示产出不同规范字节")
	}
}

func TestEventIDFromCanonical(t *testing.T) {
	// 独立已知向量：sha256("release") 的全长小写 hex。
	want := "a4d451ec23463726f72c43d64c710968f6b602cd653b4de8adee1b556240a829"
	if got := EventIDFromCanonical([]byte("release")); got != want {
		t.Fatalf("已知向量不符: got %s want %s", got, want)
	}
	a, b := validEvent(), validEvent()
	if EventIDFromCanonical(CanonicalJSON(a)) != EventIDFromCanonical(CanonicalJSON(b)) {
		t.Fatalf("同一事件两次派生 id 不一致")
	}
	b.EventID = "evt-other"
	if EventIDFromCanonical(CanonicalJSON(a)) == EventIDFromCanonical(CanonicalJSON(b)) {
		t.Fatalf("不同事件派生出相同 id")
	}
	if n := len(EventIDFromCanonical(CanonicalJSON(a))); n != 64 {
		t.Fatalf("id 长度 = %d, 期望 64", n)
	}
}
