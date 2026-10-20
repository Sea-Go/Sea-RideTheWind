package structure

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Anchor validates a locator against the tree and the same source bytes used
// to derive it, returning the verified quote character range. Quotes anchor
// into paragraph nodes only: headings organize the document but never carry
// evidence.
//
// Resolution order:
//
//  1. SectionPath (when non-empty) must match a chain of headings from the
//     document root, each next heading strictly deeper than the previous.
//  2. ParaIndex then selects the paragraph node with that global ordinal; it
//     must lie inside the resolved section's span (or the whole document
//     when SectionPath is empty).
//  3. Quote must be a substring of that paragraph's text, non-empty, and at
//     most MaxQuoteRunes runes.
func Anchor(t *Tree, source []byte, loc Locator) (AnchoredQuote, error) {
	if t == nil {
		return AnchoredQuote{}, fmt.Errorf("structure: tree is nil")
	}
	if loc.ParaIndex < 0 {
		return AnchoredQuote{}, fmt.Errorf("structure: para index %d must be >= 0", loc.ParaIndex)
	}
	sectionStart, sectionEnd, err := resolveSection(t, loc.SectionPath)
	if err != nil {
		return AnchoredQuote{}, err
	}
	var node *Node
	for i := range t.Nodes {
		n := &t.Nodes[i]
		if n.Level != LevelParagraph || n.ParaIndex != loc.ParaIndex {
			continue
		}
		if n.CharStart >= sectionStart && n.CharEnd <= sectionEnd {
			node = n
			break
		}
	}
	if node == nil {
		return AnchoredQuote{}, fmt.Errorf("structure: para %d not found inside section %q", loc.ParaIndex, strings.Join(loc.SectionPath, " / "))
	}
	if loc.Quote == "" {
		return AnchoredQuote{}, fmt.Errorf("structure: quote is empty")
	}
	if utf8.RuneCountInString(loc.Quote) > MaxQuoteRunes {
		return AnchoredQuote{}, fmt.Errorf("structure: quote is %d runes, cap is %d", utf8.RuneCountInString(loc.Quote), MaxQuoteRunes)
	}
	if node.CharStart < 0 || node.CharEnd > len(source) {
		return AnchoredQuote{}, fmt.Errorf("structure: node %s range outside source", node.NodeID)
	}
	text := string(source[node.CharStart:node.CharEnd])
	off := strings.Index(text, loc.Quote)
	if off < 0 {
		return AnchoredQuote{}, fmt.Errorf("structure: quote not present in paragraph %d (node %s)", loc.ParaIndex, node.NodeID)
	}
	return AnchoredQuote{
		NodeID:    node.NodeID,
		CharStart: node.CharStart + off,
		CharEnd:   node.CharStart + off + len(loc.Quote),
	}, nil
}

// resolveSection walks the heading chain and returns the section's covering
// span: from the first matched heading's start to just before the next
// heading with a level no deeper than the last matched one (or +∞). Each
// step searches only within the span of the previously matched heading, so
// a deeper heading nested under a sibling section can never satisfy the
// chain.
func resolveSection(t *Tree, path []string) (int, int, error) {
	if len(path) == 0 {
		return 0, 1 << 62, nil
	}
	start, level := 0, 0
	limit := 1 << 62
	for wantIdx, want := range path {
		found := -1
		for i := range t.Nodes {
			n := &t.Nodes[i]
			if n.Level == LevelParagraph || n.CharStart < start {
				continue
			}
			if n.CharStart >= limit {
				break
			}
			if n.Title != want || n.Level <= level {
				continue
			}
			found = i
			break
		}
		if found < 0 {
			return 0, 0, fmt.Errorf("structure: section path element %d %q not found", wantIdx, want)
		}
		matched := t.Nodes[found]
		start = matched.CharStart
		level = matched.Level
		limit = sectionEnd(t, start, level)
	}
	return start, limit, nil
}

// sectionEnd returns the start of the next heading with level <= level after
// start, or +∞ when the section runs to the end of the document.
func sectionEnd(t *Tree, start, level int) int {
	for i := range t.Nodes {
		n := &t.Nodes[i]
		if n.Level == LevelParagraph || n.CharStart <= start {
			continue
		}
		if n.Level <= level {
			return n.CharStart
		}
	}
	return 1 << 62
}
