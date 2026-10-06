package freeze

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// RevisionStore persists frozen revisions (C-14 input side). Contract:
// Save is idempotent for an identical value and must refuse to overwrite a
// revision id with different content — revisions are immutable.
type RevisionStore interface {
	Load(ctx context.Context, revID string) (FrozenRevision, error)
	Save(ctx context.Context, fr FrozenRevision) error
}

// StructureStore persists canonical structure trees keyed by revision id.
// Contract: SaveTree must accept only canonical bytes whose sha256 matches
// treeSHA, be idempotent for an identical pair, and refuse a different tree
// for a revision that already has one. LoadTree returns the stored canonical
// bytes and their tree sha; ErrNotFound when the revision was never frozen.
type StructureStore interface {
	SaveTree(ctx context.Context, treeJSON []byte, treeSHA string) error
	LoadTree(ctx context.Context, revID string) ([]byte, string, error)
}

// MemoryRevisionStore is the in-memory RevisionStore: a map guarded by a
// mutex. Dev/test only — the production implementation is the PG
// structure/revisions tables of the assembly milestone.
type MemoryRevisionStore struct {
	mu   sync.Mutex
	revs map[string]FrozenRevision
}

// NewMemoryRevisionStore returns an empty in-memory revision store.
func NewMemoryRevisionStore() *MemoryRevisionStore {
	return &MemoryRevisionStore{revs: make(map[string]FrozenRevision)}
}

// Load returns the frozen revision or ErrNotFound.
func (s *MemoryRevisionStore) Load(_ context.Context, revID string) (FrozenRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fr, ok := s.revs[revID]
	if !ok {
		return FrozenRevision{}, fmt.Errorf("%w: revision %q", ErrNotFound, revID)
	}
	return cloneRevision(fr), nil
}

// Save stores fr; an identical re-Save is a no-op, a divergent one is a
// conflict (immutability).
func (s *MemoryRevisionStore) Save(_ context.Context, fr FrozenRevision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.revs[fr.RevisionID]
	if !ok {
		s.revs[fr.RevisionID] = cloneRevision(fr)
		return nil
	}
	if !sameRevision(old, fr) {
		return fmt.Errorf("%w: revision %q already stored with different content", ErrConflict, fr.RevisionID)
	}
	return nil
}

// MemoryStructureStore is the in-memory StructureStore. The canonical bytes
// are fully re-parsed on SaveTree: the store itself guarantees it only ever
// holds canonical form keyed by the revision embedded in the header.
type MemoryStructureStore struct {
	mu    sync.Mutex
	trees map[string]treeEntry
}

type treeEntry struct {
	json []byte
	sha  string
}

// NewMemoryStructureStore returns an empty in-memory structure store.
func NewMemoryStructureStore() *MemoryStructureStore {
	return &MemoryStructureStore{trees: make(map[string]treeEntry)}
}

// SaveTree validates that treeJSON is canonical (parses and re-renders to
// the same bytes, sha256 matching treeSHA), extracts the revision id from
// the header, and stores the entry. Identical re-saves are no-ops;
// divergent trees for one revision are conflicts.
func (s *MemoryStructureStore) SaveTree(_ context.Context, treeJSON []byte, treeSHA string) error {
	tree, err := parseCanonicalTree(treeJSON)
	if err != nil {
		return fmt.Errorf("freeze: store tree: %w", err)
	}
	if !bytes.Equal(canonicalTreeJSON(tree), treeJSON) {
		return fmt.Errorf("freeze: store tree: bytes are not in canonical form")
	}
	if got := sha256Hex(treeJSON); got != treeSHA {
		return fmt.Errorf("freeze: store tree: sha %s does not match bytes (%s)", treeSHA, got)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.trees[tree.RevisionID]
	if ok {
		if old.sha != treeSHA || !bytes.Equal(old.json, treeJSON) {
			return fmt.Errorf("%w: revision %q already has a different tree", ErrConflict, tree.RevisionID)
		}
		return nil
	}
	s.trees[tree.RevisionID] = treeEntry{json: append([]byte(nil), treeJSON...), sha: treeSHA}
	return nil
}

// LoadTree returns a copy of the stored canonical bytes and their tree sha.
func (s *MemoryStructureStore) LoadTree(_ context.Context, revID string) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.trees[revID]
	if !ok {
		return nil, "", fmt.Errorf("%w: tree of revision %q", ErrNotFound, revID)
	}
	return append([]byte(nil), e.json...), e.sha, nil
}

func cloneRevision(fr FrozenRevision) FrozenRevision {
	fr.Source = append([]byte(nil), fr.Source...)
	return fr
}

func sameRevision(a, b FrozenRevision) bool {
	return a.RevisionID == b.RevisionID &&
		a.ModuleID == b.ModuleID &&
		a.DocKey == b.DocKey &&
		a.ContentSHA256 == b.ContentSHA256 &&
		bytes.Equal(a.Source, b.Source)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
