// Package testenv creates isolated schemas only when explicitly requested by integration tests.
package testenv

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Store(t *testing.T) *model.Store {
	t.Helper()
	dsn := os.Getenv("KNOWLEDGE_TEST_DSN")
	if dsn == "" {
		t.Skip("set KNOWLEDGE_TEST_DSN to a dedicated local PostgreSQL instance")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "knowledge_test_" + uuid.New().String()
	schema = "\"" + schema + "\""
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	c, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	c.ConnConfig.RuntimeParams["search_path"] = schema
	c.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		if err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := model.New(pool, objects)
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}
func Profiles() []types.RetrievalProfile {
	return []types.RetrievalProfile{
		{Lane: "dense", Encoder: "fixture-dense-v1", Tokenizer: "fixture-tokenizer-v1", Space: "dense-space-v1", Dimensions: 8},
		{Lane: "sparse", Encoder: "fixture-sparse-v1", Tokenizer: "fixture-tokenizer-v1", Space: "sparse-space-v1", Dimensions: 1000},
		{Lane: "multivector", Encoder: "fixture-mv-v1", Tokenizer: "fixture-tokenizer-v1", Space: "token-space-v1", Dimensions: 8, Mask: "nonpadding-v1", Aggregation: "maxsim"},
	}
}
func Put(t *testing.T, s *model.Store, value any) model.ArtifactRef {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	key, hash, err := s.Objects.Put(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	return model.ArtifactRef{Key: key, SHA256: hash}
}
func Index(t *testing.T, s *model.Store, b types.Build, r types.Release) model.IndexManifest {
	t.Helper()
	m := model.IndexManifest{SchemaVersion: 1, BuildID: b.BuildId, ReleaseID: r.ReleaseId, Generation: b.Generation, InputManifestHash: r.ManifestHash, ChunkCount: 1, ChunkManifest: Put(t, s, struct {
		RevisionIDs []string `json:"revision_ids"`
		Fixture     bool     `json:"fixture"`
	}{r.SourceRevisionIds, true})}
	for _, p := range r.RetrievalProfiles {
		m.Lanes = append(m.Lanes, model.LaneManifest{Profile: p, ChunkCount: 1, Shards: 1, ProbePassed: true, Artifact: Put(t, s, struct {
			Lane    string `json:"lane"`
			Fixture bool   `json:"fixture"`
		}{p.Lane, true}), Probe: Put(t, s, struct {
			Lane     string `json:"lane"`
			Evidence string `json:"evidence"`
		}{p.Lane, "structural acceptance fixture; no model/index probe executed"})})
	}
	return m
}
func Ready(t *testing.T, s *model.Store, b types.Build, r types.Release) types.Build {
	t.Helper()
	ctx := context.Background()
	claimed, err := s.ClaimBuild(ctx, types.ClaimBuildReq{LeaseExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano), BuildId: b.BuildId, Generation: b.Generation, AttemptId: "attempt-1", LeaseEpoch: 1, ManifestHash: b.ManifestHash})
	if err != nil {
		t.Fatal(err)
	}
	a := Put(t, s, Index(t, s, claimed, r))
	ready, err := s.AcceptBuild(ctx, types.AcceptBuildReq{BuildId: claimed.BuildId, Generation: claimed.Generation, AttemptId: claimed.AttemptId, LeaseEpoch: claimed.LeaseEpoch, ManifestHash: claimed.ManifestHash, State: "READY", IndexManifestRef: a.Key, IndexManifestHash: a.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	return ready
}
