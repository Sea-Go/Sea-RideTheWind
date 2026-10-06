package structure

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// buildSample constructs a deterministic markdown document with the given
// number of sections (each: one h2 + two paragraphs of two lines) and
// returns the source plus expectations for verification.
func buildSample(sections int) []byte {
	var b bytes.Buffer
	b.WriteString("# 总目录\n\n")
	b.WriteString("导言段落，两行组成。\n第二行仍属于导言。\n\n")
	for i := 0; i < sections; i++ {
		fmt.Fprintf(&b, "## 第 %d 节\n\n", i)
		fmt.Fprintf(&b, "第 %d 节第一段：包含可引用的事实句 alpha-%d。\n", i, i)
		b.WriteString("同节第二行。\n\n")
		fmt.Fprintf(&b, "第 %d 节第二段：事实句 beta-%d 在此。\n", i, i)
		b.WriteString("\n")
	}
	return b.Bytes()
}

func TestDeriveBasicShape(t *testing.T) {
	src := buildSample(2)
	tree, err := Derive("rev-basic", src)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	// 1 h1 + 1 intro para + per section: 1 h2 + 2 paras => 2*3
	if got, want := len(tree.Nodes), 1+1+2*3; got != want {
		t.Fatalf("nodes = %d, want %d", got, want)
	}
	if tree.Nodes[0].Level != 1 || tree.Nodes[0].Title != "总目录" {
		t.Fatalf("root heading = %+v", tree.Nodes[0])
	}
	if tree.Nodes[1].Level != LevelParagraph || tree.Nodes[1].ParaIndex != 0 {
		t.Fatalf("intro para = %+v", tree.Nodes[1])
	}
	// Paragraph ordinals are global and headings never consume them.
	wantPara := 0
	for _, n := range tree.Nodes {
		if n.Level != LevelParagraph {
			if n.ParaIndex != HeadingAbsent {
				t.Fatalf("heading %q has para index %d", n.Title, n.ParaIndex)
			}
			continue
		}
		if n.ParaIndex != wantPara {
			t.Fatalf("para ordinal = %d, want %d", n.ParaIndex, wantPara)
		}
		wantPara++
	}
	if wantPara != 5 {
		t.Fatalf("total paragraphs = %d, want 5", wantPara)
	}
}

func TestDeriveNodeTextRoundTrip(t *testing.T) {
	src := buildSample(3)
	tree, err := Derive("rev-roundtrip", src)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	for i, n := range tree.Nodes {
		text, err := tree.NodeText(src, i)
		if err != nil {
			t.Fatalf("NodeText(%d): %v", i, err)
		}
		got := src[n.CharStart:n.CharEnd]
		if text != string(got) {
			t.Fatalf("node %d text mismatch", i)
		}
		if n.Level == LevelParagraph {
			trimmed := strings.TrimSpace(text)
			if trimmed == "" {
				t.Fatalf("node %d paragraph is blank", i)
			}
			if !strings.HasPrefix(text, trimmed[:1]) {
				// Paragraph text may not start with whitespace.
				t.Fatalf("node %d paragraph starts with space: %q", i, text[:8])
			}
		}
	}
}

func TestDeriveDeterministicAndUnique(t *testing.T) {
	// 100 sections => 1 h1 + 1 intro + 100*(1 h2 + 2 paras) = 302 nodes.
	src := buildSample(100)
	a, err := Derive("rev-det", src)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	b, err := Derive("rev-det", src)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if len(a.Nodes) != 302 {
		t.Fatalf("nodes = %d, want 302 (300+ acceptance floor)", len(a.Nodes))
	}
	for i := range a.Nodes {
		if a.Nodes[i] != b.Nodes[i] {
			t.Fatalf("nondeterministic at node %d", i)
		}
	}
	seen := make(map[string]struct{}, len(a.Nodes))
	for i, n := range a.Nodes {
		if _, dup := seen[n.NodeID]; dup {
			t.Fatalf("duplicate node id %s at %d", n.NodeID, i)
		}
		seen[n.NodeID] = struct{}{}
		if len(n.NodeID) != 16 {
			t.Fatalf("node id len = %d", len(n.NodeID))
		}
		if _, err := hex.DecodeString(n.NodeID); err != nil {
			t.Fatalf("node id not hex: %v", err)
		}
	}
	// Different revision => different ids for the same source.
	c, _ := Derive("rev-other", src)
	if c.Nodes[0].NodeID == a.Nodes[0].NodeID {
		t.Fatal("node id must bind to revision id")
	}
}

func TestDeriveHeadingRules(t *testing.T) {
	src := []byte("  ### 三级标题 ###\n正文。\n\n######六级无空格不是标题\n仍正文。\n")
	tree, err := Derive("rev-headings", src)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if len(tree.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3 (1 heading + 2 paras separated by blank line)", len(tree.Nodes))
	}
	h := tree.Nodes[0]
	if h.Level != 3 || h.Title != "三级标题" {
		t.Fatalf("heading = %+v", h)
	}
	if h.CharStart != 0 || h.CharEnd != len("  ### 三级标题 ###") {
		t.Fatalf("heading range [%d,%d)", h.CharStart, h.CharEnd)
	}
	// "######六级无空格" lacks the required space, so it stays paragraph text.
	p := tree.Nodes[2]
	if p.Level != LevelParagraph || p.ParaIndex != 1 {
		t.Fatalf("para = %+v", p)
	}
	if !strings.Contains(string(src[p.CharStart:p.CharEnd]), "######六级无空格不是标题") {
		t.Fatalf("para text = %q", src[p.CharStart:p.CharEnd])
	}
}

func TestDeriveRejectsEmptyRevision(t *testing.T) {
	if _, err := Derive("", []byte("x")); err == nil {
		t.Fatal("expected error for empty revision id")
	}
}
