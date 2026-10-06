package citation

import (
	"reflect"
	"strings"
	"testing"

	"sea-try-go/service/knowledge/internal/structure"
)

const fixtureRev = "rev-citation-0001"

// citationFixture derives a multi-level fixture tree:
//
//	# 手册            (para 0 导言)
//	## 检索           (para 1, 2)
//	### 锚定          (para 3)
//	## 验收           (para 4)
func citationFixture(t *testing.T) (*structure.Tree, []byte) {
	t.Helper()
	src := []byte(`# Sea 知识手册

导言段落：知识平台以整篇文档为检索单元。

## 检索

检索第一段：C42 整篇不切割是平台红线。

检索第二段：locator 由 SectionPath 与 ParaIndex 寻址。

### 锚定

锚定段落：quote 必须是段落文本的子串且不超过 rune 上限。

## 验收

验收段落：验收记录需附 TreeSHA 与锚定字符区间。
`)
	tree, err := structure.Derive(fixtureRev, src)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if got := len(tree.Paragraphs()); got != 5 {
		t.Fatalf("fixture paragraphs = %d, want 5", got)
	}
	return tree, src
}

func srcCitation() Citation {
	return Citation{
		DocKey:     "source:0b1b2b3b-1111-2222-3333-444455556666@v1",
		RevisionID: fixtureRev,
		Locator:    structure.Locator{ParaIndex: 1, Quote: "C42 整篇不切割"},
	}
}

func pageCitation() Citation {
	return Citation{
		DocKey:     "page:0b1b2b3b-1111-2222-3333-444455557777@v1",
		RevisionID: fixtureRev,
		Locator: structure.Locator{
			SectionPath: []string{"检索", "锚定"},
			ParaIndex:   3,
			Quote:       "段落文本的子串",
		},
	}
}

func baseRequest() AcceptRequest {
	return AcceptRequest{
		AnswerID:  "ans-001",
		SearchID:  "search-001",
		Citations: []Citation{srcCitation()},
	}
}

func TestAcceptPositiveWithDedup(t *testing.T) {
	tree, src := citationFixture(t)
	dup := srcCitation() // same anchor triple, different DocKey
	dup.DocKey = "summary:0b1b2b3b-1111-2222-3333-444455558888@v1"
	req := baseRequest()
	req.Citations = append(req.Citations, pageCitation(), dup)

	got, err := Accept(tree, src, req)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if got.AnswerID != "ans-001" || got.SearchID != "search-001" {
		t.Fatalf("receipt ids = %q/%q", got.AnswerID, got.SearchID)
	}
	if len(got.Verified) != 2 {
		t.Fatalf("verified = %d citations, want 2 after dedup", len(got.Verified))
	}
	// First occurrences win, in request order.
	if got.Verified[0].DocKey != srcCitation().DocKey || got.Verified[1].DocKey != pageCitation().DocKey {
		t.Fatalf("dedup kept wrong order: [%s, %s]", got.Verified[0].DocKey, got.Verified[1].DocKey)
	}
	for _, v := range got.Verified {
		if v.Anchored.NodeID == "" {
			t.Fatal("verified citation has empty anchored node id")
		}
		if q := string(src[v.Anchored.CharStart:v.Anchored.CharEnd]); q != v.Locator.Quote {
			t.Fatalf("anchored bytes %q != locator quote %q", q, v.Locator.Quote)
		}
	}
	if len(got.TreeSHA) != 64 {
		t.Fatalf("tree sha %q is not 64 hex chars", got.TreeSHA)
	}
}

func TestAcceptCitationCapBoundary(t *testing.T) {
	tree, src := citationFixture(t)
	req := baseRequest()
	for len(req.Citations) < MaxCitations {
		req.Citations = append(req.Citations, pageCitation())
	}
	// Exactly MaxCitations citations (all anchoring to the same triple)
	// must pass and collapse to one verified entry.
	got, err := Accept(tree, src, req)
	if err != nil {
		t.Fatalf("Accept at cap: %v", err)
	}
	if len(got.Verified) != 2 { // src quote + page quote
		t.Fatalf("verified = %d, want 2", len(got.Verified))
	}
}

func TestAcceptRejections(t *testing.T) {
	tree, src := citationFixture(t)
	tooLong := strings.Repeat("长", structure.MaxQuoteRunes+1)
	cases := []struct {
		name   string
		mutate func(*AcceptRequest)
		want   string // required substring of the error
	}{
		{"empty answer id", func(r *AcceptRequest) { r.AnswerID = "" }, "answer id is required"},
		{"empty search id", func(r *AcceptRequest) { r.SearchID = "" }, "search id is required"},
		{"no citations", func(r *AcceptRequest) { r.Citations = nil }, "at least one citation"},
		{"over cap", func(r *AcceptRequest) {
			for len(r.Citations) <= MaxCitations {
				r.Citations = append(r.Citations, srcCitation())
			}
		}, "exceed the cap"},
		{"revision mismatch on item 2", func(r *AcceptRequest) {
			r.Citations = append(r.Citations, srcCitation())
			r.Citations[1].RevisionID = "rev-other-0009"
		}, "item 2: revision id"},
		{"doc key missing at on item 2", func(r *AcceptRequest) {
			r.Citations = append(r.Citations, srcCitation())
			r.Citations[1].DocKey = "source:0b1b2b3b-1111-2222-3333-444455556666v1"
		}, "item 2: doc key"},
		{"doc key unknown prefix on item 2", func(r *AcceptRequest) {
			r.Citations = append(r.Citations, srcCitation())
			r.Citations[1].DocKey = "vector:0b1b2b3b-1111-2222-3333-444455556666@v1"
		}, "item 2: doc key"},
		{"doc key empty revision", func(r *AcceptRequest) {
			r.Citations[0].DocKey = "source:0b1b2b3b-1111-2222-3333-444455556666@"
		}, "item 1: doc key"},
		{"quote not in the addressed paragraph", func(r *AcceptRequest) {
			r.Citations[0].Locator = structure.Locator{ParaIndex: 4, Quote: "C42 整篇不切割"}
		}, "item 1: anchor"},
		{"quote over 200 runes", func(r *AcceptRequest) {
			r.Citations[0].Locator.Quote = tooLong
		}, "item 1: anchor"},
		{"section path not found", func(r *AcceptRequest) {
			r.Citations[0].Locator.SectionPath = []string{"不存在的章节"}
		}, "item 1: anchor"},
		{"nil tree", nil, "tree is nil"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := baseRequest()
			if tc.mutate != nil {
				tc.mutate(&req)
			}
			receipt, err := Accept(tree, src, req)
			if tc.name == "nil tree" {
				_, err = Accept(nil, src, req)
			}
			if err == nil {
				t.Fatalf("Accept succeeded, want error containing %q (receipt %+v)", tc.want, receipt)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestAcceptDeterministic(t *testing.T) {
	tree, src := citationFixture(t)
	req := baseRequest()
	req.Citations = append(req.Citations, pageCitation(), srcCitation())

	first, err := Accept(tree, src, req)
	if err != nil {
		t.Fatalf("first Accept: %v", err)
	}
	second, err := Accept(tree, src, req)
	if err != nil {
		t.Fatalf("second Accept: %v", err)
	}
	if !deepEqualReceipt(first, second) {
		t.Fatalf("two accepts on identical input differ:\n%+v\n%+v", first, second)
	}

	// Re-deriving the tree from the same (revisionID, source) must reproduce
	// the very same TreeSHA: the fingerprint is recomputable, not remembered.
	fresh, err := structure.Derive(fixtureRev, src)
	if err != nil {
		t.Fatalf("re-Derive: %v", err)
	}
	if freshSHA := treeSHA(fresh); freshSHA != first.TreeSHA {
		t.Fatalf("re-derived tree sha %s != receipt sha %s", freshSHA, first.TreeSHA)
	}

	// A different revision of the same bytes must fingerprint differently.
	other, err := structure.Derive("rev-citation-0002", src)
	if err != nil {
		t.Fatalf("Derive other: %v", err)
	}
	otherReq := baseRequest()
	otherReq.Citations = []Citation{srcCitation()}
	otherReq.Citations[0].RevisionID = "rev-citation-0002"
	otherReceipt, err := Accept(other, src, otherReq)
	if err != nil {
		t.Fatalf("Accept other: %v", err)
	}
	if otherReceipt.TreeSHA == first.TreeSHA {
		t.Fatal("different revisions produced the same TreeSHA")
	}
}

func deepEqualReceipt(a, b Receipt) bool {
	if a.AnswerID != b.AnswerID || a.SearchID != b.SearchID || a.TreeSHA != b.TreeSHA {
		return false
	}
	if len(a.Verified) != len(b.Verified) {
		return false
	}
	for i := range a.Verified {
		if !reflect.DeepEqual(a.Verified[i], b.Verified[i]) {
			return false
		}
	}
	return true
}

func TestValidateDocKey(t *testing.T) {
	valid := []string{
		"source:0b1b2b3b-1111-2222-3333-444455556666@v1",
		"page:0B1B2B3B-1111-2222-3333-444455556666@2026-10-06T04:39Z",
		"summary:0b1b2b3b-1111-2222-3333-444455556666@r2",
	}
	for _, k := range valid {
		if err := ValidateDocKey(k); err != nil {
			t.Errorf("ValidateDocKey(%q) = %v, want nil", k, err)
		}
	}
	invalid := []string{
		"",                                    // empty
		"source:0b1b2b3b-1111-2222-3333",      // truncated uuid
		"source:0b1b2b3b11112222333344445555", // no dashes
		"vector:0b1b2b3b-1111-2222-3333-444455556666@v1",  // unknown prefix
		"source:0b1b2b3b-1111-2222-3333-444455556666v1",   // missing @
		"source:0b1b2b3b-1111-2222-3333-444455556666@",    // empty rev
		"source:0b1b2b3b-1111-2222-3333-444455556666@a b", // whitespace in rev
		"page:0b1b2b3b-1111-2222-3333-444455556666@v1 x",  // trailing junk
	}
	for _, k := range invalid {
		if err := ValidateDocKey(k); err == nil {
			t.Errorf("ValidateDocKey(%q) = nil, want error", k)
		}
	}
}

func TestCanonicalTreeJSONExactBytes(t *testing.T) {
	// Hand-built tree so the canonical bytes can be asserted literally:
	// fixed field order, no indentation, '\n' line endings, minimal string
	// escaping, trailing newline.
	tree := &structure.Tree{
		RevisionID: "rev\"x\\y\n",
		Nodes: []structure.Node{
			{NodeID: "n1", Level: 1, Title: "标 题\"引号\"", ParaIndex: structure.HeadingAbsent, CharStart: 0, CharEnd: 10},
			{NodeID: "n2", Level: structure.LevelParagraph, Title: "", ParaIndex: 0, CharStart: 11, CharEnd: 30},
		},
	}
	want := "{\"revision_id\":\"rev\\\"x\\\\y\\n\"}\n" +
		"{\"node_id\":\"n1\",\"level\":1,\"title\":\"标 题\\\"引号\\\"\",\"para_index\":-1,\"char_start\":0,\"char_end\":10}\n" +
		"{\"node_id\":\"n2\",\"level\":7,\"title\":\"\",\"para_index\":0,\"char_start\":11,\"char_end\":30}\n"
	if got := string(canonicalTreeJSON(tree)); got != want {
		t.Fatalf("canonical json:\n got %q\nwant %q", got, want)
	}
	// Byte-for-byte stable across calls.
	if string(canonicalTreeJSON(tree)) != want {
		t.Fatal("canonical json is not stable across calls")
	}
}
