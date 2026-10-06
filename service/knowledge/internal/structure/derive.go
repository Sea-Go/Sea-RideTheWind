package structure

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Derive builds the DocStructureTree of one revision from its markdown
// source. The function is pure and deterministic: it only reads revisionID
// and source, and identical inputs always produce identical trees.
//
// Derivation rules (kept intentionally small and testable):
//
//   - A heading line is ≤3 leading spaces, 1..6 '#', one required space,
//     then the title text. A trailing closing sequence of '#' preceded by a
//     space is dropped (CommonMark-style ATX). Heading nodes span their full
//     line and never consume paragraph ordinals.
//   - Blank lines separate paragraphs. A paragraph node spans from the first
//     non-space character of its first line to the end of its last line.
//     Paragraph ordinals are global across the document.
//   - Fenced code blocks are treated as paragraph content: the platform
//     freezes markdown revisions verbatim, and code is evidence like prose.
//     (No syntax awareness beyond heading detection.)
//   - node_id = hex(sha256(revisionID || 0x00 || decimal(seq)))[0:16].
func Derive(revisionID string, source []byte) (*Tree, error) {
	if revisionID == "" {
		return nil, fmt.Errorf("structure: revision id is required")
	}
	t := &Tree{RevisionID: revisionID}
	seq := 0
	paraIndex := 0
	lines := splitLines(source)
	i := 0
	for i < len(lines) {
		ln := lines[i]
		if level, title, ok := parseHeading(ln.text); ok {
			id := nodeID(revisionID, seq)
			t.Nodes = append(t.Nodes, Node{
				NodeID:    id,
				Level:     level,
				Title:     title,
				ParaIndex: HeadingAbsent,
				CharStart: ln.start,
				CharEnd:   ln.end,
			})
			seq++
			i++
			continue
		}
		if strings.TrimSpace(ln.text) == "" {
			i++
			continue
		}
		// Paragraph: consume until blank line or heading line.
		first := i
		last := i
		for last+1 < len(lines) {
			nxt := lines[last+1]
			if _, _, ok := parseHeading(nxt.text); ok {
				break
			}
			if strings.TrimSpace(nxt.text) == "" {
				break
			}
			last++
		}
		start := lines[first].start + leadingSpaces(lines[first].text)
		end := lines[last].end
		id := nodeID(revisionID, seq)
		t.Nodes = append(t.Nodes, Node{
			NodeID:    id,
			Level:     LevelParagraph,
			ParaIndex: paraIndex,
			CharStart: start,
			CharEnd:   end,
		})
		paraIndex++
		seq++
		i = last + 1
	}
	return t, nil
}

// line is one source line with its byte range [start, end).
type line struct {
	text       string
	start, end int
}

func splitLines(source []byte) []line {
	var out []line
	start := 0
	for i := 0; i < len(source); i++ {
		if source[i] == '\n' {
			end := i
			if end > start && source[end-1] == '\r' {
				end--
			}
			out = append(out, line{text: string(source[start:end]), start: start, end: end})
			start = i + 1
		}
	}
	if start < len(source) {
		out = append(out, line{text: string(source[start:]), start: start, end: len(source)})
	}
	return out
}

func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && (s[n] == ' ' || s[n] == '\t') {
		n++
	}
	return n
}

// parseHeading recognizes an ATX heading per the documented rules.
func parseHeading(s string) (level int, title string, ok bool) {
	lead := 0
	for lead < len(s) && lead < 3 && s[lead] == ' ' {
		lead++
	}
	rest := s[lead:]
	hashes := 0
	for hashes < len(rest) && rest[hashes] == '#' && hashes < 6 {
		hashes++
	}
	if hashes == 0 || hashes > 6 {
		return 0, "", false
	}
	rest = rest[hashes:]
	if rest == "" || rest[0] != ' ' {
		return 0, "", false
	}
	title = strings.TrimRight(rest[1:], " \t")
	// Drop an optional closing sequence of '#' preceded by whitespace.
	if idx := strings.LastIndex(title, " #"); idx >= 0 {
		candidate := strings.TrimRight(title[idx+1:], " #")
		if candidate == "" {
			title = strings.TrimRight(title[:idx], " \t")
		}
	}
	return hashes, title, true
}

func nodeID(revisionID string, seq int) string {
	h := sha256.New()
	h.Write([]byte(revisionID))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(seq)))
	return hex.EncodeToString(h.Sum(nil))[:16]
}
