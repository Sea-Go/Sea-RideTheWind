package citation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"sea-try-go/service/knowledge/internal/structure"
)

// MaxCitations is the hard cap of citations one acceptance may carry.
const MaxCitations = 32

// anchorKey is the dedup identity of a verified citation: the same anchored
// triple means two citations point at the exact same evidence, regardless of
// their DocKey or quote phrasing.
type anchorKey struct {
	nodeID    string
	charStart int
	charEnd   int
}

// Accept validates an AcceptRequest against the frozen revision's tree and
// the exact source bytes that derived it, returning the acceptance receipt.
//
// Rules, in order:
//
//  1. tree must be non-nil; AnswerID and SearchID must be non-empty.
//  2. Citations must hold 1..MaxCitations entries.
//  3. Every citation must carry the tree's RevisionID, a well-formed DocKey,
//     and a locator that structure.Anchor verifies against (tree, source).
//  4. Citations anchoring to the same (NodeID, CharStart, CharEnd) triple
//     are merged (first occurrence wins); duplicates are not an error.
//
// Any violation aborts the whole acceptance with an error naming the
// 1-based citation index and the reason; only a fully valid request yields
// a Receipt. Receipt.TreeSHA is hex(sha256(canonical tree JSON)).
func Accept(tree *structure.Tree, source []byte, req AcceptRequest) (Receipt, error) {
	if tree == nil {
		return Receipt{}, fmt.Errorf("citation: tree is nil")
	}
	if req.AnswerID == "" {
		return Receipt{}, fmt.Errorf("citation: answer id is required")
	}
	if req.SearchID == "" {
		return Receipt{}, fmt.Errorf("citation: search id is required")
	}
	if len(req.Citations) == 0 {
		return Receipt{}, fmt.Errorf("citation: at least one citation is required")
	}
	if len(req.Citations) > MaxCitations {
		return Receipt{}, fmt.Errorf("citation: %d citations exceed the cap of %d", len(req.Citations), MaxCitations)
	}

	seen := make(map[anchorKey]struct{}, len(req.Citations))
	verified := make([]VerifiedCitation, 0, len(req.Citations))
	for i, c := range req.Citations {
		if c.RevisionID != tree.RevisionID {
			return Receipt{}, fmt.Errorf("citation: item %d: revision id %q does not match tree revision %q",
				i+1, c.RevisionID, tree.RevisionID)
		}
		if err := ValidateDocKey(c.DocKey); err != nil {
			return Receipt{}, fmt.Errorf("citation: item %d: %w", i+1, err)
		}
		anchored, err := structure.Anchor(tree, source, c.Locator)
		if err != nil {
			return Receipt{}, fmt.Errorf("citation: item %d: anchor: %w", i+1, err)
		}
		k := anchorKey{nodeID: anchored.NodeID, charStart: anchored.CharStart, charEnd: anchored.CharEnd}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		verified = append(verified, VerifiedCitation{Citation: c, Anchored: anchored})
	}

	return Receipt{
		AnswerID: req.AnswerID,
		SearchID: req.SearchID,
		Verified: verified,
		TreeSHA:  treeSHA(tree),
	}, nil
}

// treeSHA fingerprints a tree as hex(sha256(canonical JSON)).
func treeSHA(t *structure.Tree) string {
	sum := sha256.Sum256(canonicalTreeJSON(t))
	return hex.EncodeToString(sum[:])
}

// canonicalTreeJSON renders a tree in the platform's canonical form: one
// compact JSON record per line — the revision header first, then every node
// in document order — with fields in fixed order, no indentation, '\n' line
// endings, and a trailing newline.
//
// The encoder is hand-rolled on purpose: the byte form must never depend on
// encoding/json implementation details (map iteration order, HTML escaping
// defaults), because Receipt.TreeSHA has to be recomputable forever.
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

// writeCanonicalString appends s as a minimal-escaping JSON string: only
// quote, backslash, and control bytes are escaped; UTF-8 passes through
// verbatim, and no HTML escaping is applied.
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
