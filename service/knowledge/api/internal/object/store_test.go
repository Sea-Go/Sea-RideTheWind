package object

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestImmutableLocalObjects(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	data := []byte("Book A\n\nEvidence")
	key, hash, err := s.Put(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			k, h, e := s.Put(ctx, data)
			if e != nil || k != key || h != hash {
				t.Errorf("duplicate put=%s %s %v", k, h, e)
			}
		}()
	}
	wg.Wait()
	for _, bad := range []string{"../secret", "sha256/../secret", "https://example.com/file"} {
		if _, err = s.Get(ctx, bad, hash); err == nil {
			t.Fatal("invalid key read")
		}
	}
	if err = os.WriteFile(filepath.Join(dir, key), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, key, hash); err == nil {
		t.Fatal("tampered object accepted")
	}
	if _, _, err = s.Put(ctx, data); err == nil {
		t.Fatal("immutable collision overwritten")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err = s.Put(cancelled, []byte("new")); err == nil {
		t.Fatal("cancelled put succeeded")
	}
}
