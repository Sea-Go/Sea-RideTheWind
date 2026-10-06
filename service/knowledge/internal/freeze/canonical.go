package freeze

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"sea-try-go/service/knowledge/internal/structure"
)

// canonicalTreeJSON renders t in the canonical tree form: one compact JSON
// record per line — the revision header first, then every node in document
// order — with fields in fixed order, no indentation, '\n' line endings,
// and a trailing newline.
//
// The byte format mirrors the canonical form the citation package
// fingerprints (Receipt.TreeSHA): same field order, same minimal escaping.
// The two encoders are deliberately independent implementations of one
// documented format, and the Release→Accept round-trip test pins them
// together — freeze's treeSHA must equal citation's Receipt.TreeSHA for the
// same tree, forever, because both are audit fingerprints.
//
// Like citation's encoder, this one is hand-rolled: the byte form must never
// depend on encoding/json implementation details (map iteration order, HTML
// escaping defaults).
func canonicalTreeJSON(t *structure.Tree) []byte {
	var b strings.Builder
	b.WriteString(`{"revision_id":`)
	writeCanonicalString(&b, t.RevisionID)
	b.WriteString("}\n")
	for i := range t.Nodes {
		n := &t.Nodes[i]
		b.WriteString(`{"node_id":`)
		writeCanonicalString(&b, n.NodeID)
		fmt.Fprintf(&b, `,"level":%d,"title":`, n.Level)
		writeCanonicalString(&b, n.Title)
		fmt.Fprintf(&b, `,"para_index":%d,"char_start":%d,"char_end":%d}`+"\n",
			n.ParaIndex, n.CharStart, n.CharEnd)
	}
	return []byte(b.String())
}

// writeCanonicalString appends s as a minimally escaped JSON string: only
// quote, backslash, and control bytes are escaped; UTF-8 passes through
// verbatim; no HTML escaping.
func writeCanonicalString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
}

// parseCanonicalTree restores a structure.Tree from the canonical byte form
// produced by canonicalTreeJSON. The parser is strict: it accepts only the
// exact line grammar the writer emits (fixed key order, minimal escapes,
// no trailing junk, '\n'-terminated lines), so parse(canonical(t)) is
// guaranteed deep-equal to t and the pair forms a byte-round-trip.
func parseCanonicalTree(data []byte) (*structure.Tree, error) {
	p := &canonicalParser{data: string(data)}
	if err := p.literal(`{"revision_id":`); err != nil {
		return nil, fmt.Errorf("freeze: canonical header: %w", err)
	}
	revID, err := p.quotedString()
	if err != nil {
		return nil, fmt.Errorf("freeze: canonical header: %w", err)
	}
	if err := p.literal("}\n"); err != nil {
		return nil, fmt.Errorf("freeze: canonical header: %w", err)
	}
	t := &structure.Tree{RevisionID: revID}
	for p.pos < len(p.data) {
		n, err := parseCanonicalNode(p)
		if err != nil {
			return nil, err
		}
		t.Nodes = append(t.Nodes, n)
	}
	return t, nil
}

func parseCanonicalNode(p *canonicalParser) (structure.Node, error) {
	if err := p.literal(`{"node_id":`); err != nil {
		return structure.Node{}, fmt.Errorf("freeze: canonical node: %w", err)
	}
	nodeID, err := p.quotedString()
	if err != nil {
		return structure.Node{}, fmt.Errorf("freeze: canonical node: %w", err)
	}
	if err := p.literal(`,"level":`); err != nil {
		return structure.Node{}, fmt.Errorf("freeze: canonical node: %w", err)
	}
	level, err := p.integer()
	if err != nil {
		return structure.Node{}, fmt.Errorf("freeze: canonical node level: %w", err)
	}
	if err := p.literal(`,"title":`); err != nil {
		return structure.Node{}, fmt.Errorf("freeze: canonical node: %w", err)
	}
	title, err := p.quotedString()
	if err != nil {
		return structure.Node{}, fmt.Errorf("freeze: canonical node: %w", err)
	}
	if err := p.literal(`,"para_index":`); err != nil {
		return structure.Node{}, fmt.Errorf("freeze: canonical node: %w", err)
	}
	para, err := p.integer()
	if err != nil {
		return structure.Node{}, fmt.Errorf("freeze: canonical node para_index: %w", err)
	}
	if err := p.literal(`,"char_start":`); err != nil {
		return structure.Node{}, fmt.Errorf("freeze: canonical node: %w", err)
	}
	start, err := p.integer()
	if err != nil {
		return structure.Node{}, fmt.Errorf("freeze: canonical node char_start: %w", err)
	}
	if err := p.literal(`,"char_end":`); err != nil {
		return structure.Node{}, fmt.Errorf("freeze: canonical node: %w", err)
	}
	end, err := p.integer()
	if err != nil {
		return structure.Node{}, fmt.Errorf("freeze: canonical node char_end: %w", err)
	}
	if err := p.literal("}\n"); err != nil {
		return structure.Node{}, fmt.Errorf("freeze: canonical node: %w", err)
	}
	if level < 1 || level > structure.LevelParagraph {
		return structure.Node{}, fmt.Errorf("freeze: canonical node %s: level %d outside 1..%d", nodeID, level, structure.LevelParagraph)
	}
	if para < structure.HeadingAbsent {
		return structure.Node{}, fmt.Errorf("freeze: canonical node %s: para_index %d below %d", nodeID, para, structure.HeadingAbsent)
	}
	if level != structure.LevelParagraph && para != structure.HeadingAbsent {
		return structure.Node{}, fmt.Errorf("freeze: canonical node %s: heading carries para_index %d", nodeID, para)
	}
	if start < 0 || end < start {
		return structure.Node{}, fmt.Errorf("freeze: canonical node %s: invalid span [%d,%d)", nodeID, start, end)
	}
	return structure.Node{
		NodeID:    nodeID,
		Level:     level,
		Title:     title,
		ParaIndex: para,
		CharStart: start,
		CharEnd:   end,
	}, nil
}

// canonicalParser consumes the canonical byte form left to right. Every
// method returns an error naming the offset on mismatch.
type canonicalParser struct {
	data string
	pos  int
}

func (p *canonicalParser) literal(s string) error {
	if !strings.HasPrefix(p.data[p.pos:], s) {
		return fmt.Errorf("expected %q at offset %d", s, p.pos)
	}
	p.pos += len(s)
	return nil
}

// quotedString parses a JSON string in the writer's minimal-escape dialect:
// \" \\ \n \r \t \uXXXX plus verbatim bytes. Raw control bytes, lone
// surrogates, and unknown escapes are rejected — the writer never emits
// them, so seeing one means the bytes are not canonical.
func (p *canonicalParser) quotedString() (string, error) {
	if err := p.literal(`"`); err != nil {
		return "", err
	}
	var b []byte
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			return string(b), nil
		case c == '\\':
			p.pos++
			if p.pos >= len(p.data) {
				return "", fmt.Errorf("unterminated escape at offset %d", p.pos)
			}
			e := p.data[p.pos]
			switch e {
			case '"', '\\':
				b = append(b, e)
				p.pos++
			case 'n':
				b = append(b, '\n')
				p.pos++
			case 'r':
				b = append(b, '\r')
				p.pos++
			case 't':
				b = append(b, '\t')
				p.pos++
			case 'u':
				if p.pos+5 > len(p.data) {
					return "", fmt.Errorf("truncated \\u escape at offset %d", p.pos)
				}
				v, err := strconv.ParseUint(p.data[p.pos+1:p.pos+5], 16, 32)
				if err != nil {
					return "", fmt.Errorf("bad \\u escape at offset %d: %v", p.pos, err)
				}
				r := rune(v)
				if r >= 0xD800 && r <= 0xDFFF {
					return "", fmt.Errorf("surrogate \\u escape at offset %d is not canonical", p.pos)
				}
				b = utf8.AppendRune(b, r)
				p.pos += 5
			default:
				return "", fmt.Errorf("unknown escape \\%c at offset %d", e, p.pos)
			}
		case c < 0x20:
			return "", fmt.Errorf("raw control byte 0x%02x at offset %d is not canonical", c, p.pos)
		default:
			b = append(b, c)
			p.pos++
		}
	}
	return "", fmt.Errorf("unterminated string at offset %d", p.pos)
}

// integer parses the writer's decimal form: optional '-', digits, no
// leading zeros (a lone "0" is fine), no "-0".
func (p *canonicalParser) integer() (int, error) {
	start := p.pos
	if p.pos < len(p.data) && p.data[p.pos] == '-' {
		p.pos++
	}
	digits := 0
	for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
		p.pos++
		digits++
	}
	if digits == 0 {
		return 0, fmt.Errorf("expected integer at offset %d", start)
	}
	text := p.data[start:p.pos]
	if text == "-0" {
		return 0, fmt.Errorf("integer %q at offset %d is not canonical", text, start)
	}
	mantissa := strings.TrimPrefix(text, "-")
	if len(mantissa) > 1 && mantissa[0] == '0' {
		return 0, fmt.Errorf("integer %q at offset %d has a leading zero", text, start)
	}
	v, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("integer %q at offset %d out of range", text, start)
	}
	if int64(int(v)) != v {
		return 0, fmt.Errorf("integer %q at offset %d exceeds int", text, start)
	}
	return int(v), nil
}
