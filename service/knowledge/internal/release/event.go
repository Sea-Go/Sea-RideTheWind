// Package release 是发布域的纯域层（C-1 事件唯一写者）：
// 发布事件契约、C08 门禁状态机与 Outbox 接口。不接触 DB/网络/时钟，
// 时间一律由调用方注入，本包只做无副作用的校验与派生。
package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// Event 是一次发布对外宣告的不可变事实（契约 v2）。
// 字段顺序即契约顺序，JSON tag 全部 snake_case，三轨必须一致。
type Event struct {
	EventID     string     `json:"event_id"`
	ModuleID    string     `json:"module_id"`
	ReleaseID   string     `json:"release_id"`
	PublishedAt time.Time  `json:"published_at"`
	Docs        []EventDoc `json:"docs"`
}

// EventDoc 是事件内一个已冻结文档的引用四元组 + 内容指纹。
type EventDoc struct {
	DocKey        string `json:"doc_key"`
	RevisionID    string `json:"revision_id"`
	S3Ref         string `json:"s3_ref"`
	StructureRef  string `json:"structure_ref"`
	ContentSHA256 string `json:"content_sha256"`
	CharLen       int    `json:"char_len"`
}

// docKeyPattern 匹配 source:<uuid>@<rev> | page:<uuid>@<rev> | summary:<uuid>@<rev>。
// uuid 接受大小写 hex；rev 为仓库内修订号字符集（字母/数字/./_/-，非空）。
var docKeyPattern = regexp.MustCompile(
	`^(source|page|summary):[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}@[0-9A-Za-z._-]+$`,
)

// sha256Pattern 匹配 64 位小写十六进制。
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Validate 校验事件契约：标识非空、docs≥1、doc_key 正则、
// content_sha256 为 64 位小写 hex、published_at 非零、引用字段非空。
func (e Event) Validate() error {
	if e.EventID == "" {
		return fmt.Errorf("release: event_id 为空")
	}
	if e.ModuleID == "" {
		return fmt.Errorf("release: module_id 为空")
	}
	if e.ReleaseID == "" {
		return fmt.Errorf("release: release_id 为空")
	}
	if e.PublishedAt.IsZero() {
		return fmt.Errorf("release: published_at 为零值")
	}
	if len(e.Docs) < 1 {
		return fmt.Errorf("release: docs 至少 1 个文档")
	}
	for i, d := range e.Docs {
		if !docKeyPattern.MatchString(d.DocKey) {
			return fmt.Errorf("release: docs[%d].doc_key %q 不符合 source|page|summary:<uuid>@<rev>", i, d.DocKey)
		}
		if d.RevisionID == "" {
			return fmt.Errorf("release: docs[%d].revision_id 为空", i)
		}
		if d.S3Ref == "" {
			return fmt.Errorf("release: docs[%d].s3_ref 为空", i)
		}
		if d.StructureRef == "" {
			return fmt.Errorf("release: docs[%d].structure_ref 为空", i)
		}
		if !sha256Pattern.MatchString(d.ContentSHA256) {
			return fmt.Errorf("release: docs[%d].content_sha256 %q 不是 64 位小写 hex", i, d.ContentSHA256)
		}
		if d.CharLen < 0 {
			return fmt.Errorf("release: docs[%d].char_len 为负数", i)
		}
	}
	return nil
}

// canonicalEvent 是 CanonicalJSON 的固定字段序视图；
// published_at 规范化为 UTC 的 RFC3339（含亚秒，去掉尾随零），
// 保证同一时刻在不同时区表示下产出相同规范字节。
type canonicalEvent struct {
	EventID     string         `json:"event_id"`
	ModuleID    string         `json:"module_id"`
	ReleaseID   string         `json:"release_id"`
	PublishedAt string         `json:"published_at"`
	Docs        []canonicalDoc `json:"docs"`
}

type canonicalDoc struct {
	DocKey        string `json:"doc_key"`
	RevisionID    string `json:"revision_id"`
	S3Ref         string `json:"s3_ref"`
	StructureRef  string `json:"structure_ref"`
	ContentSHA256 string `json:"content_sha256"`
	CharLen       int    `json:"char_len"`
}

// CanonicalJSON 返回事件的规范字节：固定字段顺序（契约快照序）、
// 无缩进、docs 保持原序（不排序——事件里的顺序即发布顺序，是事实的一部分）。
// 同一 Event 值永远产出同一字节；published_at 统一为 UTC。
// 入参未校验：调用方应先 Validate（规范形对非法事件同样可计算）。
func CanonicalJSON(e Event) []byte {
	c := canonicalEvent{
		EventID:     e.EventID,
		ModuleID:    e.ModuleID,
		ReleaseID:   e.ReleaseID,
		PublishedAt: e.PublishedAt.UTC().Format(time.RFC3339Nano),
		Docs:        make([]canonicalDoc, len(e.Docs)),
	}
	for i, d := range e.Docs {
		c.Docs[i] = canonicalDoc{
			DocKey:        d.DocKey,
			RevisionID:    d.RevisionID,
			S3Ref:         d.S3Ref,
			StructureRef:  d.StructureRef,
			ContentSHA256: d.ContentSHA256,
			CharLen:       d.CharLen,
		}
	}
	// 仅 string/int 字段，json.Marshal 不会失败。
	b, _ := json.Marshal(c)
	return b
}

// EventIDFromCanonical 由规范字节派生事件标识：hex(sha256(canonical)) 全长
// （64 位小写 hex，无前缀）。event_id 本身由调用方生成，本函数只提供
// 确定性派生——同字节同 id，跨轨可比对。
func EventIDFromCanonical(canonical []byte) string {
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}
