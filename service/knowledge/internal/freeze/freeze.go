// Package freeze wires the three pure domain packages of the no-chunk wiki
// platform into the minimal runnable M1 chain:
//
//	revision freeze → structure tree derivation → release event
//
// It is the C-14 skeleton: the trigger side (who calls Freeze) and the
// storage side (who keeps revisions and trees) are injected interfaces with
// in-memory implementations; PG persistence lands in the assembly
// milestone. The package itself owns only the orchestration and its
// deterministic fingerprints:
//
//   - Freeze verifies the content sha, derives the structure tree, renders
//     the canonical tree JSON (byte-identical to the form citation
//     fingerprints) and keys the tree by treeSHA = hex(sha256(canonical)).
//   - Release freezes every doc, assembles the C-1 v2 event
//     (structure_ref = treeSHA, s3_ref = "sha256:<content sha>" placeholder),
//     derives a deterministic event_id, validates, and appends to the Outbox.
//   - Accept reloads the canonical tree and the exact source bytes, then
//     delegates to citation.Accept for C-4 quote-verified receipts.
package freeze

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sea-try-go/service/knowledge/internal/citation"
	"sea-try-go/service/knowledge/internal/release"
	"sea-try-go/service/knowledge/internal/structure"
)

// Sentinel errors carried through the store seams. Production (PG)
// implementations must return ErrNotFound for missing rows so the service
// can distinguish "never frozen" from a storage failure.
var (
	ErrNotFound = errors.New("freeze: not found")
	ErrConflict = errors.New("freeze: conflict")
)

// S3RefPrefix is the placeholder convention for EventDoc.S3Ref until the
// object-storage assembly milestone supplies real object references: the
// content-addressed form "sha256:<content sha256>" already carries the
// immutability guarantee the C-1 contract needs.
const S3RefPrefix = "sha256:"

// FrozenRevision is one document revision entering the chain: where it
// lives (ModuleID/DocKey/RevisionID) and its exact bytes plus content
// fingerprint. Source is the single source of truth; ContentSHA256 must
// equal hex(sha256(Source)) — Freeze rejects anything else.
type FrozenRevision struct {
	RevisionID    string
	ModuleID      string
	DocKey        string
	Source        []byte
	ContentSHA256 string
}

// Service orchestrates the freeze→structure→release chain over injected
// stores. It holds no state of its own beyond the store handles, so it is
// safe for concurrent use as far as the stores are.
type Service struct {
	revisions  RevisionStore
	structures StructureStore
	outbox     release.Store
}

// NewService assembles a Service from the three storage seams. Nil stores
// are wiring bugs and panic immediately.
func NewService(revisions RevisionStore, structures StructureStore, outbox release.Store) *Service {
	if revisions == nil {
		panic("freeze: NewService: RevisionStore is nil")
	}
	if structures == nil {
		panic("freeze: NewService: StructureStore is nil")
	}
	if outbox == nil {
		panic("freeze: NewService: Outbox store is nil")
	}
	return &Service{revisions: revisions, structures: structures, outbox: outbox}
}

// NewMemoryService wires the chain entirely in memory (dev/test skeleton).
func NewMemoryService() *Service {
	return NewService(NewMemoryRevisionStore(), NewMemoryStructureStore(), release.NewMemoryStore())
}

// Freeze derives and persists the structure tree of one frozen revision,
// returning treeSHA = hex(sha256(canonical tree JSON)).
//
// Steps: validate the revision envelope, verify ContentSHA256 against the
// source bytes, derive the tree, render canonical bytes, and persist. A
// replay with the same revID whose stored tree SHA matches the recomputed
// one returns immediately (idempotent); a divergent tree for the same
// revID is a conflict because revisions are immutable.
func (s *Service) Freeze(ctx context.Context, fr FrozenRevision) (string, error) {
	if err := validateFrozenRevision(fr); err != nil {
		return "", err
	}
	actual := sha256Hex(fr.Source)
	if actual != fr.ContentSHA256 {
		return "", fmt.Errorf("freeze: revision %s: content sha256 %s does not match source bytes (%s)",
			fr.RevisionID, fr.ContentSHA256, actual)
	}
	tree, err := structure.Derive(fr.RevisionID, fr.Source)
	if err != nil {
		return "", fmt.Errorf("freeze: derive revision %s: %w", fr.RevisionID, err)
	}
	canonical := canonicalTreeJSON(tree)
	treeSHA := sha256Hex(canonical)

	storedJSON, storedSHA, err := s.structures.LoadTree(ctx, fr.RevisionID)
	if err == nil {
		if storedSHA != treeSHA {
			return "", fmt.Errorf("%w: revision %s already frozen with tree sha %s, refusing tree sha %s",
				ErrConflict, fr.RevisionID, storedSHA, treeSHA)
		}
		if sha256Hex(storedJSON) != storedSHA {
			return "", fmt.Errorf("freeze: stored tree of revision %s failed its integrity check", fr.RevisionID)
		}
		if err := s.ensureRevision(ctx, fr); err != nil {
			return "", err
		}
		return storedSHA, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("freeze: load tree of revision %s: %w", fr.RevisionID, err)
	}

	if err := s.structures.SaveTree(ctx, canonical, treeSHA); err != nil {
		return "", fmt.Errorf("freeze: save tree of revision %s: %w", fr.RevisionID, err)
	}
	if err := s.revisions.Save(ctx, fr); err != nil {
		return "", fmt.Errorf("freeze: save revision %s: %w", fr.RevisionID, err)
	}
	return treeSHA, nil
}

// ensureRevision closes the crash window where a tree was stored but the
// revision was not: replays re-Save the revision, which is idempotent for
// identical records and reports ErrConflict when the envelope (ModuleID or
// DocKey) drifted — the tree alone cannot detect that, because Derive only
// reads RevisionID and Source.
func (s *Service) ensureRevision(ctx context.Context, fr FrozenRevision) error {
	if err := s.revisions.Save(ctx, fr); err != nil {
		return fmt.Errorf("freeze: save revision %s: %w", fr.RevisionID, err)
	}
	return nil
}

// Release freezes every doc (idempotent), assembles the C-1 v2 event,
// derives its deterministic event_id, validates it, and appends it to the
// Outbox. It returns the event id.
//
// event_id derivation: the canonical form of the event with the event_id
// field still empty is the preimage; event_id = EventIDFromCanonical of
// those bytes, stamped back into the event before Validate/Append. The
// same (module, release, docs in order, published_at) therefore always
// yields the same id — docs order is part of the published fact — and an
// identical replay is a no-op in the outbox (Append is idempotent by
// event_id, per the C-1 idempotency key).
func (s *Service) Release(ctx context.Context, moduleID, releaseID string, docs []FrozenRevision, at time.Time) (string, error) {
	if moduleID == "" {
		return "", fmt.Errorf("freeze: module id is required")
	}
	if releaseID == "" {
		return "", fmt.Errorf("freeze: release id is required")
	}
	if len(docs) == 0 {
		return "", fmt.Errorf("freeze: release needs at least one doc")
	}
	if at.IsZero() {
		return "", fmt.Errorf("freeze: published_at is required")
	}

	event := release.Event{
		ModuleID:    moduleID,
		ReleaseID:   releaseID,
		PublishedAt: at,
		Docs:        make([]release.EventDoc, len(docs)),
	}
	for i, d := range docs {
		if d.ModuleID != moduleID {
			return "", fmt.Errorf("freeze: doc %d: module %q does not match release module %q", i+1, d.ModuleID, moduleID)
		}
		treeSHA, err := s.Freeze(ctx, d)
		if err != nil {
			return "", fmt.Errorf("freeze: doc %d: %w", i+1, err)
		}
		event.Docs[i] = release.EventDoc{
			DocKey:        d.DocKey,
			RevisionID:    d.RevisionID,
			S3Ref:         S3RefPrefix + d.ContentSHA256,
			StructureRef:  treeSHA,
			ContentSHA256: d.ContentSHA256,
			CharLen:       len(d.Source),
		}
	}

	event.EventID = release.EventIDFromCanonical(release.CanonicalJSON(event))
	if err := event.Validate(); err != nil {
		return "", fmt.Errorf("freeze: release event: %w", err)
	}
	if err := s.outbox.Append(event); err != nil {
		return "", fmt.Errorf("freeze: append outbox: %w", err)
	}
	return event.EventID, nil
}

// Accept verifies a citation request against the frozen revision revID:
// the canonical tree is loaded and re-parsed, the source bytes are loaded
// from the revision store and re-hashed, each citation's DocKey is checked
// against the frozen revision's own DocKey, and citation.Accept does the
// C-4 quote-hit validation. The receipt's TreeSHA is checked against the stored
// treeSHA — the two canonical encoders must agree forever, and a drift is a
// hard error, not a warning.
func (s *Service) Accept(ctx context.Context, req citation.AcceptRequest, revID string) (citation.Receipt, error) {
	canonical, treeSHA, err := s.structures.LoadTree(ctx, revID)
	if err != nil {
		return citation.Receipt{}, fmt.Errorf("freeze: load tree of revision %s: %w", revID, err)
	}
	if sha256Hex(canonical) != treeSHA {
		return citation.Receipt{}, fmt.Errorf("freeze: stored tree of revision %s failed its integrity check", revID)
	}
	tree, err := parseCanonicalTree(canonical)
	if err != nil {
		return citation.Receipt{}, fmt.Errorf("freeze: stored tree of revision %s is not canonical: %w", revID, err)
	}
	if tree.RevisionID != revID {
		return citation.Receipt{}, fmt.Errorf("%w: stored tree holds revision %s, requested %s", ErrConflict, tree.RevisionID, revID)
	}
	fr, err := s.revisions.Load(ctx, revID)
	if err != nil {
		return citation.Receipt{}, fmt.Errorf("freeze: load revision %s: %w", revID, err)
	}
	if sha256Hex(fr.Source) != fr.ContentSHA256 {
		return citation.Receipt{}, fmt.Errorf("freeze: source bytes of revision %s no longer match their content sha", revID)
	}

	for i, c := range req.Citations {
		if c.DocKey != fr.DocKey {
			return citation.Receipt{}, fmt.Errorf(
				"freeze: citation %d doc key %q does not match frozen document %q",
				i+1, c.DocKey, fr.DocKey,
			)
		}
	}

	receipt, err := citation.Accept(tree, fr.Source, req)
	if err != nil {
		return citation.Receipt{}, err
	}
	if receipt.TreeSHA != treeSHA {
		return citation.Receipt{}, fmt.Errorf("freeze: citation tree sha %s disagrees with frozen tree sha %s for revision %s",
			receipt.TreeSHA, treeSHA, revID)
	}
	return receipt, nil
}

func validateFrozenRevision(fr FrozenRevision) error {
	if fr.RevisionID == "" {
		return fmt.Errorf("freeze: revision id is required")
	}
	if fr.ModuleID == "" {
		return fmt.Errorf("freeze: revision %s: module id is required", fr.RevisionID)
	}
	if err := citation.ValidateDocKey(fr.DocKey); err != nil {
		return fmt.Errorf("freeze: revision %s: %w", fr.RevisionID, err)
	}
	if !isLowerHex64(fr.ContentSHA256) {
		return fmt.Errorf("freeze: revision %s: content sha256 %q must be 64 lowercase hex chars", fr.RevisionID, fr.ContentSHA256)
	}
	return nil
}

func isLowerHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
