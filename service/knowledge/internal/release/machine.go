package release

import (
	"fmt"
	"time"
)

// Status 是发布单的 C08 门禁状态。
type Status string

const (
	StatusDraft      Status = "draft"
	StatusConfirmed  Status = "confirmed"
	StatusPublished  Status = "published"
	StatusRolledBack Status = "rolled_back"
)

// Release 是一个模块某次发布的门禁视图（纯数据，无行为副作用）。
type Release struct {
	ModuleID    string
	ReleaseID   string
	Status      Status
	PublishedAt time.Time
	DocKeys     []string
}

// transitions 是唯一合法迁移表：draft→confirmed、confirmed→published、
// published→rolled_back。其余一律非法：重复发布、跳级、回退后复活。
// rolled_back 为终态（无出边）。
var transitions = map[Status]map[Status]bool{
	StatusDraft:      {StatusConfirmed: true},
	StatusConfirmed:  {StatusPublished: true},
	StatusPublished:  {StatusRolledBack: true},
	StatusRolledBack: {},
}

// Transition 返回迁移后的新 Release，不修改入参（值语义拷贝）。
// 仅 confirmed→published 记录 PublishedAt（要求 at 非零）；
// 其余合法迁移不改时间字段。非法迁移返回错误与原 Release 原样。
func Transition(r Release, to Status, at time.Time) (Release, error) {
	legal, known := transitions[r.Status]
	if !known {
		return r, fmt.Errorf("release: 未知来源状态 %q", r.Status)
	}
	if !legal[to] {
		return r, fmt.Errorf("release: 非法迁移 %q → %q", r.Status, to)
	}
	if to == StatusPublished && at.IsZero() {
		return r, fmt.Errorf("release: %q → %q 要求非零 at", r.Status, to)
	}
	next := r
	next.Status = to
	if to == StatusPublished {
		next.PublishedAt = at
	}
	return next, nil
}
