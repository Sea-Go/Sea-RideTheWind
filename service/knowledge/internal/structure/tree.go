// Package structure derives the immutable DocStructureTree of a frozen
// revision and anchors locator quotes to verified character ranges.
//
// The tree is the evidence-addressing substrate of the no-chunk LLM Wiki
// platform (C42): retrieval units stay whole documents, while structure
// nodes only carry locator metadata. Derivation is a pure, deterministic
// function of (revisionID, source): same input always yields the same tree,
// which makes frozen revisions reproducible and auditable.
//
// Node identity is sha256(revisionID + NUL + decimal(seq)) truncated to 16
// hex characters, guaranteeing per-revision uniqueness without coordination.
package structure

import "fmt"

// MaxQuoteRunes is the hard cap for a locator quote anchor.
const MaxQuoteRunes = 200

// Node levels. Headings use ATX levels 1..6; paragraphs are level 7 and are
// the only nodes that may carry quotes.
const (
	LevelParagraph = 7
	HeadingAbsent  = -1
)

// Node is one entry of a DocStructureTree. CharStart/CharEnd are byte
// offsets into the exact source bytes passed to Derive ([start, end)).
type Node struct {
	NodeID    string
	Level     int    // 1..6 for headings, LevelParagraph for paragraphs
	Title     string // heading text, trimmed; empty for paragraphs
	ParaIndex int    // global paragraph ordinal; HeadingAbsent for headings
	CharStart int
	CharEnd   int
}

// Tree is the derived structure of one frozen revision.
type Tree struct {
	RevisionID string
	Nodes      []Node
}

// Locator addresses evidence inside one revision. SectionPath is the ordered
// chain of heading titles from the document root; ParaIndex is the global
// paragraph ordinal. A zero SectionPath with ParaIndex >= 0 addresses the
// paragraph directly.
type Locator struct {
	SectionPath []string
	ParaIndex   int
	Quote       string
}

// AnchoredQuote is the verified result of anchoring a locator quote.
type AnchoredQuote struct {
	NodeID    string
	CharStart int
	CharEnd   int
}

// NodeText returns the source bytes covered by node i.
func (t *Tree) NodeText(source []byte, i int) (string, error) {
	if t == nil || i < 0 || i >= len(t.Nodes) {
		return "", fmt.Errorf("structure: node index %d out of range", i)
	}
	n := t.Nodes[i]
	if n.CharStart < 0 || n.CharEnd > len(source) || n.CharStart > n.CharEnd {
		return "", fmt.Errorf("structure: node %s range [%d,%d) outside source of %d bytes", n.NodeID, n.CharStart, n.CharEnd, len(source))
	}
	return string(source[n.CharStart:n.CharEnd]), nil
}

// Paragraphs returns the indexes of all paragraph nodes in document order.
func (t *Tree) Paragraphs() []int {
	var out []int
	for i, n := range t.Nodes {
		if n.Level == LevelParagraph {
			out = append(out, i)
		}
	}
	return out
}
