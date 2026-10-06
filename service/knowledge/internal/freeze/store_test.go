package freeze

import (
	"context"
	"errors"
	"strings"
	"testing"

	"sea-try-go/service/knowledge/internal/structure"
)

func TestMemoryRevisionStore(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryRevisionStore()
	fr := docA()

	if _, err := s.Load(ctx, fr.RevisionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.Save(ctx, fr); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Identical save is a no-op; a mutated clone is a conflict.
	mutated := fr
	mutated.Source = append(append([]byte(nil), fr.Source...), '!')
	mutated.ContentSHA256 = sha256Hex(mutated.Source)
	if err := s.Save(ctx, mutated); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if err := s.Save(ctx, cloneRevision(fr)); err != nil {
		t.Fatalf("idempotent save: %v", err)
	}
	loaded, err := s.Load(ctx, fr.RevisionID)
	if err != nil || !sameRevision(loaded, fr) {
		t.Fatalf("load: %v %+v", err, loaded)
	}
	// The store keeps its own copy: mutating the returned bytes is inert.
	loaded.Source[0] = 'X'
	again, err := s.Load(ctx, fr.RevisionID)
	if err != nil || !sameRevision(again, fr) {
		t.Fatalf("store was corrupted by caller mutation: %v", err)
	}
}

func TestMemoryStructureStore(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStructureStore()
	fr := docA()
	tree, canonical := derivedCanonical(t, fr)
	sha := sha256Hex(canonical)

	if _, _, err := s.LoadTree(ctx, fr.RevisionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.SaveTree(ctx, canonical, sha); err != nil {
		t.Fatalf("save tree: %v", err)
	}
	if err := s.SaveTree(ctx, append([]byte(nil), canonical...), sha); err != nil {
		t.Fatalf("idempotent save: %v", err)
	}

	// Wrong sha for the bytes is rejected.
	if err := s.SaveTree(ctx, canonical, strings.Repeat("0", 64)); err == nil ||
		!strings.Contains(err.Error(), "does not match bytes") {
		t.Fatalf("expected sha mismatch, got %v", err)
	}
	// Non-canonical bytes are rejected even with a matching sha.
	nonCanonical := strings.Replace(string(canonical), `{"node_id":`, `{"node_id" :`, 1)
	if err := s.SaveTree(ctx, []byte(nonCanonical), sha256Hex([]byte(nonCanonical))); err == nil {
		t.Fatal("expected non-canonical bytes to be rejected")
	}

	gotJSON, gotSHA, err := s.LoadTree(ctx, tree.RevisionID)
	if err != nil {
		t.Fatalf("load tree: %v", err)
	}
	if gotSHA != sha || string(gotJSON) != string(canonical) {
		t.Fatalf("loaded tree mismatch")
	}
	// Returned bytes are a copy.
	gotJSON[0] = ' '
	if fresh, _, _ := s.LoadTree(ctx, tree.RevisionID); string(fresh) != string(canonical) {
		t.Fatal("store corrupted by caller mutation")
	}
	// A second revision with a different tree coexists.
	other := docB()
	_, otherCanonical := derivedCanonical(t, other)
	if err := s.SaveTree(ctx, otherCanonical, sha256Hex(otherCanonical)); err != nil {
		t.Fatalf("save second tree: %v", err)
	}
	// Same revision, different tree → conflict.
	divergent := mkFrozen("mod-1", fr.RevisionID, fr.DocKey, "# Book A\n\nDifferent tree.\n")
	_, divergentCanonical := derivedCanonical(t, divergent)
	if err := s.SaveTree(ctx, divergentCanonical, sha256Hex(divergentCanonical)); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func derivedCanonical(t *testing.T, fr FrozenRevision) (*structure.Tree, []byte) {
	t.Helper()
	tree, err := structure.Derive(fr.RevisionID, fr.Source)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	return tree, canonicalTreeJSON(tree)
}
