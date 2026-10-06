package release

import (
	"reflect"
	"testing"
	"time"
)

var transitionAt = time.Date(2026, 10, 6, 5, 30, 0, 0, time.UTC)

func draftRelease() Release {
	return Release{
		ModuleID:  "module_h04",
		ReleaseID: "rel-20261006-001",
		Status:    StatusDraft,
		DocKeys: []string{
			"source:11111111-1111-4111-8111-111111111111@revision_book_a_v1",
			"summary:33333333-3333-4333-8333-333333333333@revision_sum_v2",
		},
	}
}

func TestTransitionAllSixteenCombinations(t *testing.T) {
	statuses := []Status{StatusDraft, StatusConfirmed, StatusPublished, StatusRolledBack}
	legal := map[Status]Status{
		StatusDraft:     StatusConfirmed,
		StatusConfirmed: StatusPublished,
		StatusPublished: StatusRolledBack,
	}
	legalCount := 0
	for _, from := range statuses {
		for _, to := range statuses {
			r := draftRelease()
			r.Status = from
			if from == StatusPublished || from == StatusRolledBack {
				r.PublishedAt = transitionAt
			}
			next, err := Transition(r, to, transitionAt)
			if to == legal[from] {
				legalCount++
				if err != nil {
					t.Fatalf("合法迁移 %s → %s 报错: %v", from, to, err)
				}
				if next.Status != to {
					t.Fatalf("迁移后状态 = %q, 期望 %q", next.Status, to)
				}
			} else {
				if err == nil {
					t.Fatalf("非法迁移 %s → %s 被放行", from, to)
				}
				if !reflect.DeepEqual(next, r) {
					t.Fatalf("非法迁移 %s → %s 修改了原状态", from, to)
				}
			}
		}
	}
	if legalCount != 3 {
		t.Fatalf("合法迁移数 = %d, 期望 3", legalCount)
	}
}

func TestTransitionRecordsPublishedAt(t *testing.T) {
	r := draftRelease()
	confirmed, err := Transition(r, StatusConfirmed, transitionAt)
	if err != nil {
		t.Fatalf("draft→confirmed: %v", err)
	}
	if !confirmed.PublishedAt.IsZero() {
		t.Fatalf("draft→confirmed 不应写 PublishedAt")
	}
	published, err := Transition(confirmed, StatusPublished, transitionAt)
	if err != nil {
		t.Fatalf("confirmed→published: %v", err)
	}
	if !published.PublishedAt.Equal(transitionAt) {
		t.Fatalf("PublishedAt = %v, 期望 %v", published.PublishedAt, transitionAt)
	}
	rolled, err := Transition(published, StatusRolledBack, transitionAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("published→rolled_back: %v", err)
	}
	if !rolled.PublishedAt.Equal(transitionAt) {
		t.Fatalf("rolled_back 应保留 PublishedAt 历史")
	}
	if !reflect.DeepEqual(rolled.DocKeys, r.DocKeys) {
		t.Fatalf("迁移不应改动 DocKeys")
	}
}

func TestTransitionRejectsZeroPublishTime(t *testing.T) {
	r := draftRelease()
	r.Status = StatusConfirmed
	if _, err := Transition(r, StatusPublished, time.Time{}); err == nil {
		t.Fatalf("零值 at 的发布应被拒绝")
	}
}

func TestTransitionRejectsUnknownStatus(t *testing.T) {
	r := draftRelease()
	r.Status = Status("shipped")
	if _, err := Transition(r, StatusConfirmed, transitionAt); err == nil {
		t.Fatalf("未知来源状态应被拒绝")
	}
	if _, err := Transition(draftRelease(), Status("shipped"), transitionAt); err == nil {
		t.Fatalf("未知目标状态应被拒绝")
	}
}
