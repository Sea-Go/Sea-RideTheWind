package structure

import (
	"strings"
	"testing"
)

func anchorFixture(t *testing.T) (*Tree, []byte) {
	t.Helper()
	src := []byte(`# 手册

导言。

## 安装

安装第一段：运行 brew install sea 即可完成。

安装第二段：环境变量 SEA_HOME 控制数据目录。

### 进阶

进阶段落：调优参数 tuning-x 见此。
`)
	tree, err := Derive("rev-anchor", src)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	return tree, src
}

func TestAnchorPlainParagraph(t *testing.T) {
	tree, src := anchorFixture(t)
	aq, err := Anchor(tree, src, Locator{ParaIndex: 1, Quote: "brew install sea"})
	if err != nil {
		t.Fatalf("Anchor: %v", err)
	}
	if got := string(src[aq.CharStart:aq.CharEnd]); got != "brew install sea" {
		t.Fatalf("anchored text = %q", got)
	}
	if aq.NodeID == "" {
		t.Fatal("anchored node id is empty")
	}
}

func TestAnchorSectionPathResolution(t *testing.T) {
	tree, src := anchorFixture(t)
	// "进阶段落" lives under ## 安装 > ### 进阶 with global para index 3.
	aq, err := Anchor(tree, src, Locator{
		SectionPath: []string{"安装", "进阶"},
		ParaIndex:   3,
		Quote:       "tuning-x",
	})
	if err != nil {
		t.Fatalf("Anchor: %v", err)
	}
	if got := string(src[aq.CharStart:aq.CharEnd]); got != "tuning-x" {
		t.Fatalf("anchored text = %q", got)
	}

	// Same paragraph ordinal under the WRONG section must be rejected:
	// para 3 lives under 安装/进阶, not under the bare 安装 span… actually
	// 进阶 is nested inside 安装, so the section constraint should accept
	// it via the parent path but reject via an unrelated path.
	if _, err := Anchor(tree, src, Locator{
		SectionPath: []string{"不存在"},
		ParaIndex:   3,
		Quote:       "tuning-x",
	}); err == nil {
		t.Fatal("expected error for unknown section path")
	}
}

func TestAnchorSectionPathBoundedToParentSection(t *testing.T) {
	src := []byte("# A\n\npara under A.\n\n## B\n\npara under B.\n\n## C\n\n### D\n\npara under D.\n")
	tree, err := Derive("rev-sibling", src)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	// "para under D" is global para 2, nested under ## C > ### D. The chain
	// B > D must fail even though both titles exist: D lives in a sibling
	// subtree, not inside B's span.
	if _, err := Anchor(tree, src, Locator{
		SectionPath: []string{"B", "D"},
		ParaIndex:   2,
		Quote:       "para under D",
	}); err == nil {
		t.Fatal("expected error: D is nested under C, not under B")
	}
	// The honest chain resolves the same paragraph.
	aq, err := Anchor(tree, src, Locator{
		SectionPath: []string{"C", "D"},
		ParaIndex:   2,
		Quote:       "para under D",
	})
	if err != nil {
		t.Fatalf("Anchor: %v", err)
	}
	if got := string(src[aq.CharStart:aq.CharEnd]); got != "para under D" {
		t.Fatalf("anchored text = %q", got)
	}
}

func TestAnchorRejectsQuoteOutsideParagraph(t *testing.T) {
	tree, src := anchorFixture(t)
	// "导言" is para 0's text; anchoring it as para 1 must fail.
	if _, err := Anchor(tree, src, Locator{ParaIndex: 1, Quote: "导言"}); err == nil {
		t.Fatal("expected error for quote present in doc but wrong paragraph")
	}
	// Heading text is never quotable: pick a heading title as quote.
	if _, err := Anchor(tree, src, Locator{ParaIndex: 0, Quote: "安装"}); err == nil {
		t.Fatal("expected error when quote only matches a heading title elsewhere")
	}
}

func TestAnchorRejectsInvalidInput(t *testing.T) {
	tree, src := anchorFixture(t)
	cases := []struct {
		name string
		loc  Locator
	}{
		{"negative para", Locator{ParaIndex: -1, Quote: "导言"}},
		{"missing para", Locator{ParaIndex: 99, Quote: "导言"}},
		{"empty quote", Locator{ParaIndex: 0, Quote: ""}},
		{"oversized quote", Locator{ParaIndex: 0, Quote: strings.Repeat("长", MaxQuoteRunes+1)}},
		{"quote absent", Locator{ParaIndex: 0, Quote: "不存在的话"}},
	}
	for _, tc := range cases {
		if _, err := Anchor(tree, src, tc.loc); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
	if _, err := Anchor(nil, src, Locator{ParaIndex: 0, Quote: "导言"}); err == nil {
		t.Fatal("expected error for nil tree")
	}
}

func TestAnchorUnicodeOffsets(t *testing.T) {
	src := []byte("第一段包含中文事实句「海底两万里」的引用。\n")
	tree, err := Derive("rev-unicode", src)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	aq, err := Anchor(tree, src, Locator{ParaIndex: 0, Quote: "海底两万里"})
	if err != nil {
		t.Fatalf("Anchor: %v", err)
	}
	if got := string(src[aq.CharStart:aq.CharEnd]); got != "海底两万里" {
		t.Fatalf("anchored text = %q", got)
	}
}
