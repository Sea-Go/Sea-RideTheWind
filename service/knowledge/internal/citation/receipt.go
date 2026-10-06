// Package citation turns candidate answer citations into verified
// acceptance receipts against a frozen revision's structure tree (C-4).
//
// Accept is a pure function of (tree, source, request): no storage, no
// network, no clock. Identical inputs always yield a deeply equal Receipt,
// and Receipt.TreeSHA is recomputable from the tree alone, so acceptance
// records stay auditable long after the call returns.
package citation

import (
	"fmt"
	"regexp"

	"sea-try-go/service/knowledge/internal/structure"
)

// docKeyPattern matches source:<uuid>@<rev>, page:<uuid>@<rev> and
// summary:<uuid>@<rev>. The revision suffix must be non-empty and may not
// contain '@' or whitespace, so a DocKey always splits unambiguously at the
// last separator.
var docKeyPattern = regexp.MustCompile(
	`^(source|page|summary):` +
		`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-` +
		`[0-9a-fA-F]{4}-[0-9a-fA-F]{12}` +
		`@([^@\s]+)$`)

// Citation is one evidence reference of an answer: which document (DocKey),
// which frozen revision, and where in it (Locator).
type Citation struct {
	DocKey     string
	RevisionID string
	Locator    structure.Locator
}

// AcceptRequest is the input of Accept: the answer and search being
// receipted, plus the candidate citations to verify.
type AcceptRequest struct {
	AnswerID  string
	SearchID  string
	Citations []Citation
}

// Receipt is the acceptance record: every citation was verified against the
// tree, duplicates were merged, and TreeSHA fingerprints the exact tree the
// verification ran on.
type Receipt struct {
	AnswerID string
	SearchID string
	Verified []VerifiedCitation
	TreeSHA  string
}

// VerifiedCitation pairs a citation with its anchored character range. The
// quote was proven present in that node of the tree's revision.
type VerifiedCitation struct {
	Citation
	Anchored structure.AnchoredQuote
}

// ValidateDocKey checks the DocKey grammar: one of the source/page/summary
// kinds, a canonical UUID, and a non-empty revision after '@'.
func ValidateDocKey(key string) error {
	if !docKeyPattern.MatchString(key) {
		return fmt.Errorf("doc key %q must match (source|page|summary):<uuid>@<rev> with a non-empty revision", key)
	}
	return nil
}
