package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/telemetry"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

// These JSON shapes are the H07 boundary owned by BTW. RTW deliberately does
// not import BTW's internal package; every field needed for an accepted quote
// is independently checked against its publication and immutable objects.
type citationSnapshot struct {
	ModuleID            string                 `json:"module_id"`
	ReleaseID           string                 `json:"release_id"`
	Generation          int64                  `json:"generation"`
	PublicationRevision string                 `json:"publication_revision"`
	Indexes             map[string]ArtifactRef `json:"indexes"`
	ValidRevisionIDs    []string               `json:"valid_revision_ids"`
}
type citationKey struct {
	SourceKind string `json:"source_kind"`
	ContentID  string `json:"content_id"`
	RevisionID string `json:"revision_id"`
	ChunkID    string `json:"chunk_id"`
}
type citationEvidence struct {
	ID        string                 `json:"evidence_id"`
	Key       citationKey            `json:"key"`
	Locator   types.CitationLocation `json:"locator"`
	Original  types.CitationObject   `json:"original"`
	Quote     string                 `json:"quote"`
	QuoteHash string                 `json:"quote_hash"`
	Relevance float64                `json:"relevance"`
	Sources   []json.RawMessage      `json:"sources"`
}
type citationPack struct {
	SearchID       string             `json:"search_id"`
	Snapshot       citationSnapshot   `json:"snapshot"`
	Profile        json.RawMessage    `json:"profile"`
	Status         string             `json:"status"`
	StopReason     string             `json:"stop_reason"`
	CoverageStatus string             `json:"coverage_status"`
	Gaps           []string           `json:"gaps"`
	Evidence       []citationEvidence `json:"evidence"`
}
type citationChunkManifest struct {
	SchemaVersion     int                   `json:"schema_version"`
	ModuleID          string                `json:"module_id"`
	ReleaseID         string                `json:"release_id"`
	InputManifestHash string                `json:"input_manifest_hash"`
	Profile           string                `json:"profile"`
	ParserVersion     string                `json:"parser_version"`
	ChunkerVersion    string                `json:"chunker_version"`
	ChunkSize         int                   `json:"chunk_size"`
	Overlap           int                   `json:"overlap"`
	Inputs            []json.RawMessage     `json:"inputs"`
	Chunks            []types.CitationChunk `json:"chunks"`
}

func decodeCitationJSON(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return invalid("malformed citation JSON")
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return invalid("trailing citation JSON")
	}
	return nil
}

func citationHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func citationIdentity(value string) bool {
	if value == "" || len(value) > 200 {
		return false
	}
	for _, ch := range value {
		if ch != '-' && ch != '_' && ch != '.' && (ch < '0' || ch > '9') && (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') {
			return false
		}
	}
	return true
}

func (s *Store) citationPublication(ctx context.Context, snapshot citationSnapshot) (types.Release, types.Build, IndexManifest, citationChunkManifest, error) {
	var empty IndexManifest
	var emptyChunks citationChunkManifest
	pointer, err := strconv.ParseInt(snapshot.PublicationRevision, 10, 64)
	if snapshot.ModuleID == "" || snapshot.ReleaseID == "" || snapshot.Generation < 1 || err != nil || pointer < 1 ||
		snapshot.PublicationRevision != strconv.FormatInt(pointer, 10) {
		return types.Release{}, types.Build{}, empty, emptyChunks, invalid("fixed publication identity required")
	}
	release, err := s.GetHistoricalRelease(ctx, snapshot.ModuleID, snapshot.ReleaseID)
	if err != nil {
		return types.Release{}, types.Build{}, empty, emptyChunks, err
	}
	var raw []byte
	err = s.DB.QueryRow(ctx, `SELECT b.data FROM knowledge_publications p
 JOIN knowledge_builds b ON b.id=p.build_id AND b.module_id=p.module_id AND b.release_id=p.release_id
 WHERE p.module_id=$1 AND p.release_id=$2 AND p.pointer_revision=$3
 AND b.generation=$4 AND b.data->>'state'='READY'`,
		snapshot.ModuleID, snapshot.ReleaseID, pointer, snapshot.Generation).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.Release{}, types.Build{}, empty, emptyChunks, ErrNotFound
	}
	if err != nil {
		return types.Release{}, types.Build{}, empty, emptyChunks, err
	}
	var build types.Build
	if err = json.Unmarshal(raw, &build); err != nil {
		return types.Release{}, types.Build{}, empty, emptyChunks, err
	}
	if build.ManifestHash != release.ManifestHash || build.IndexManifestRef == "" || !citationHash(build.IndexManifestHash) {
		return types.Release{}, types.Build{}, empty, emptyChunks, ErrUnavailable
	}
	raw, err = s.readArtifact(ctx, build.IndexManifestRef, build.IndexManifestHash)
	if err != nil {
		return types.Release{}, types.Build{}, empty, emptyChunks, err
	}
	var index IndexManifest
	if err = decodeCitationJSON(raw, &index); err != nil {
		return types.Release{}, types.Build{}, empty, emptyChunks, err
	}
	if index.SchemaVersion != 1 || index.BuildID != build.BuildId || index.ReleaseID != release.ReleaseId ||
		index.Generation != build.Generation || index.InputManifestHash != release.ManifestHash || len(index.Lanes) != 3 {
		return types.Release{}, types.Build{}, empty, emptyChunks, ErrArtifactUnavailable
	}
	raw, err = s.readArtifact(ctx, index.ChunkManifest.Key, index.ChunkManifest.SHA256)
	if err != nil {
		return types.Release{}, types.Build{}, empty, emptyChunks, err
	}
	var chunks citationChunkManifest
	if err = decodeCitationJSON(raw, &chunks); err != nil {
		return types.Release{}, types.Build{}, empty, emptyChunks, err
	}
	if chunks.SchemaVersion != 1 || chunks.ModuleID != release.ModuleId || chunks.ReleaseID != release.ReleaseId ||
		chunks.InputManifestHash != release.ManifestHash || chunks.Profile != release.ChunkingProfile ||
		chunks.ParserVersion != "sea.paragraph.v1" || chunks.ChunkerVersion != "trpc.fixed.v1.8.1" ||
		chunks.ChunkSize < 1 || chunks.Overlap < 0 || chunks.Overlap >= chunks.ChunkSize ||
		len(chunks.Chunks) == 0 || int64(len(chunks.Chunks)) != index.ChunkCount {
		return types.Release{}, types.Build{}, empty, emptyChunks, ErrArtifactUnavailable
	}
	return release, build, index, chunks, nil
}

func citationParagraph(content string, locator string) (string, int, int, bool) {
	var ordinal int
	if _, err := fmt.Sscanf(locator, "paragraph:%d", &ordinal); err != nil || ordinal < 1 || locator != fmt.Sprintf("paragraph:%d", ordinal) {
		return "", 0, 0, false
	}
	var normalized strings.Builder
	positions := make([]int, 0, len(content)+1)
	for i := 0; i < len(content); i++ {
		positions = append(positions, i)
		if content[i] == '\r' && i+1 < len(content) && content[i+1] == '\n' {
			i++
			normalized.WriteByte('\n')
		} else {
			normalized.WriteByte(content[i])
		}
	}
	positions = append(positions, len(content))
	start, current := 0, 0
	for _, block := range strings.Split(normalized.String(), "\n\n") {
		end := start + len(block)
		if strings.TrimSpace(block) != "" {
			current++
			if current == ordinal {
				return block, positions[start], positions[end], true
			}
		}
		start = end + 2
	}
	return "", 0, 0, false
}

// The locked BTW chunker first trims the paragraph and then each line,
// normalizing CRLF and CR. A future chunker version must add an explicit
// verifier instead of silently reinterpreting old offsets.
func citationNormalize(paragraph string) string {
	paragraph = strings.ReplaceAll(strings.TrimSpace(paragraph), "\r\n", "\n")
	paragraph = strings.ReplaceAll(paragraph, "\r", "\n")
	lines := strings.Split(paragraph, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.Join(lines, "\n")
}

func sourceFromManifest(chunk types.CitationChunk, revision types.Revision) error {
	if chunk.RevisionId != revision.RevisionId || chunk.ContentId != revision.EntityId || chunk.SourceKind != revision.Kind ||
		chunk.Original.Key != revision.ObjectKey || chunk.Original.Sha256 != revision.ContentHash ||
		!citationHash(chunk.TextHash) || chunk.Text == "" || !utf8.ValidString(revision.Content) ||
		object.Hash([]byte(revision.Content)) != revision.ContentHash || object.Hash([]byte(chunk.Text)) != chunk.TextHash {
		return ErrArtifactUnavailable
	}
	block, start, end, ok := citationParagraph(revision.Content, chunk.Location.Locator)
	if !ok || chunk.Location.OriginalByteStart != start || chunk.Location.OriginalByteEnd != end {
		return ErrArtifactUnavailable
	}
	runes := []rune(citationNormalize(block))
	if chunk.Location.NormalizedRuneStart < 0 || chunk.Location.NormalizedRuneEnd > len(runes) ||
		chunk.Location.NormalizedRuneEnd <= chunk.Location.NormalizedRuneStart ||
		string(runes[chunk.Location.NormalizedRuneStart:chunk.Location.NormalizedRuneEnd]) != chunk.Text {
		return ErrArtifactUnavailable
	}
	return nil
}

func (s *Store) ReadSearchSource(ctx context.Context, req types.ReadSearchSourceReq) (types.CitationChunk, error) {
	return observe(ctx, s, "knowledge.search.source.read", "search-source:"+req.ChunkId, req, func(ctx context.Context) (types.CitationChunk, error) {
		if !citationIdentity(req.RevisionId) || !citationHash(req.ChunkId) {
			return types.CitationChunk{}, invalid("revision and chunk IDs required")
		}
		snapshot := citationSnapshot{ModuleID: req.ModuleId, ReleaseID: req.ReleaseId,
			Generation: req.Generation, PublicationRevision: req.PublicationRevision}
		release, _, _, manifest, err := s.citationPublication(ctx, snapshot)
		if err != nil {
			return types.CitationChunk{}, err
		}
		if !slices.Contains(release.SourceRevisionIds, req.RevisionId) && !slices.Contains(release.WikiRevisionIds, req.RevisionId) {
			return types.CitationChunk{}, ErrNotFound
		}
		var selected types.CitationChunk
		matches := 0
		for _, chunk := range manifest.Chunks {
			if chunk.ChunkId == req.ChunkId && chunk.RevisionId == req.RevisionId {
				selected = chunk
				matches++
			}
		}
		if matches == 0 {
			return types.CitationChunk{}, ErrNotFound
		}
		if matches != 1 {
			return types.CitationChunk{}, ErrArtifactUnavailable
		}
		revision, err := s.GetPublishedRevision(ctx, req.ModuleId, req.ReleaseId, req.RevisionId)
		if err != nil {
			return types.CitationChunk{}, err
		}
		if err = sourceFromManifest(selected, revision); err != nil {
			return types.CitationChunk{}, err
		}
		// A concurrent withdrawal cannot turn a stale object read into a quote.
		if _, err = s.historicalRelease(ctx, req.ModuleId, req.ReleaseId); err != nil {
			return types.CitationChunk{}, err
		}
		return selected, nil
	})
}

func citationEvidenceID(searchID string, key citationKey, quoteHash string) string {
	raw, _ := json.Marshal(struct {
		SearchID string
		Key      citationKey
		Hash     string
	}{searchID, key, quoteHash})
	sum := sha256.Sum256(raw)
	return "ev_" + hex.EncodeToString(sum[:])[:24]
}

func checkPackSnapshot(snapshot citationSnapshot, index IndexManifest, release types.Release) error {
	if len(snapshot.Indexes) != 3 || len(snapshot.ValidRevisionIDs) == 0 {
		return invalid("three fixed indexes and effective revisions required")
	}
	for _, lane := range index.Lanes {
		if snapshot.Indexes[lane.Profile.Lane] != lane.Artifact {
			return conflict("search index differs from published generation")
		}
	}
	allowed := append(append([]string{}, release.SourceRevisionIds...), release.WikiRevisionIds...)
	seen := map[string]bool{}
	for _, id := range snapshot.ValidRevisionIDs {
		if !slices.Contains(allowed, id) || seen[id] {
			return invalid("effective revision set differs from release")
		}
		seen[id] = true
	}
	return nil
}

func (s *Store) validateCitationPack(ctx context.Context, pack citationPack) error {
	release, _, index, _, err := s.citationPublication(ctx, pack.Snapshot)
	if err != nil {
		return err
	}
	if err = checkPackSnapshot(pack.Snapshot, index, release); err != nil {
		return err
	}
	if len(pack.Evidence) == 0 || len(pack.Evidence) > 100 {
		return invalid("nonempty bounded citation evidence required")
	}
	seen := map[string]bool{}
	for _, evidence := range pack.Evidence {
		if seen[evidence.ID] || evidence.ID != citationEvidenceID(pack.SearchID, evidence.Key, evidence.QuoteHash) ||
			!slices.Contains(pack.Snapshot.ValidRevisionIDs, evidence.Key.RevisionID) {
			return invalid("invalid or duplicated evidence identity")
		}
		seen[evidence.ID] = true
		chunk, err := s.ReadSearchSource(ctx, types.ReadSearchSourceReq{
			ModuleId: pack.Snapshot.ModuleID, ReleaseId: pack.Snapshot.ReleaseID, Generation: pack.Snapshot.Generation,
			PublicationRevision: pack.Snapshot.PublicationRevision, RevisionId: evidence.Key.RevisionID, ChunkId: evidence.Key.ChunkID,
		})
		if err != nil {
			return err
		}
		if chunk.SourceKind != evidence.Key.SourceKind || chunk.ContentId != evidence.Key.ContentID ||
			chunk.Location != evidence.Locator || chunk.Original != evidence.Original ||
			chunk.Text != evidence.Quote || chunk.TextHash != evidence.QuoteHash {
			return conflict("evidence differs from same-generation original quote")
		}
	}
	return nil
}

func (s *Store) AcceptSearchCitations(ctx context.Context, req types.AcceptSearchCitationsReq) (types.SearchCitationReceipt, error) {
	return observe(ctx, s, "knowledge.search.citations.accept", "search-citations:"+req.SearchId, req, func(ctx context.Context) (types.SearchCitationReceipt, error) {
		if !citationIdentity(req.SearchId) || !citationHash(req.PackHash) || len(req.PackJson) > 1<<20 ||
			object.Hash([]byte(req.PackJson)) != req.PackHash {
			return types.SearchCitationReceipt{}, invalid("search ID, bounded pack and exact pack hash required")
		}
		var pack citationPack
		if err := decodeCitationJSON([]byte(req.PackJson), &pack); err != nil {
			return types.SearchCitationReceipt{}, err
		}
		if pack.SearchID != req.SearchId {
			return types.SearchCitationReceipt{}, invalid("pack search ID differs")
		}
		// Replay survives a cancellation or a later withdrawal. The immutable
		// receipt can be recovered without re-reading now unavailable sources.
		previous, previousJSON, err := s.citationReceipt(ctx, req.SearchId)
		if err == nil {
			if previous.PackHash != req.PackHash || previousJSON != req.PackJson {
				return types.SearchCitationReceipt{}, conflictCode("IDEMPOTENCY_CONFLICT", "search ID reused with different evidence")
			}
			telemetry.Replay(ctx)
			return previous, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return types.SearchCitationReceipt{}, err
		}
		if err = s.validateCitationPack(ctx, pack); err != nil {
			return types.SearchCitationReceipt{}, err
		}
		tx, err := s.DB.Begin(ctx)
		if err != nil {
			return types.SearchCitationReceipt{}, err
		}
		defer tx.Rollback(context.Background())
		// All publication/withdrawal writers lock the module first. This
		// creates the citation's authoritative validity instant at commit.
		if _, err = module(ctx, tx, pack.Snapshot.ModuleID, true); err != nil {
			return types.SearchCitationReceipt{}, err
		}
		pointer, _ := strconv.ParseInt(pack.Snapshot.PublicationRevision, 10, 64)
		if err = citationEffectiveInTx(ctx, tx, pack.Snapshot, pointer); err != nil {
			return types.SearchCitationReceipt{}, err
		}
		durable := "search-citations/sha256/" + object.Hash([]byte(req.SearchId))
		receipt := types.SearchCitationReceipt{SearchId: req.SearchId, PackHash: req.PackHash, DurableRef: durable}
		tag, err := tx.Exec(ctx, `INSERT INTO knowledge_search_citations
 (search_id,pack_hash,pack_json,durable_ref,module_id,release_id,pointer_revision,generation)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(search_id) DO NOTHING`,
			req.SearchId, req.PackHash, req.PackJson, durable, pack.Snapshot.ModuleID,
			pack.Snapshot.ReleaseID, pointer, pack.Snapshot.Generation)
		if err != nil {
			return types.SearchCitationReceipt{}, err
		}
		if tag.RowsAffected() == 0 {
			var hash, raw, ref string
			if err = tx.QueryRow(ctx, "SELECT pack_hash,pack_json,durable_ref FROM knowledge_search_citations WHERE search_id=$1", req.SearchId).Scan(&hash, &raw, &ref); err != nil {
				return types.SearchCitationReceipt{}, err
			}
			if hash != req.PackHash || raw != req.PackJson {
				return types.SearchCitationReceipt{}, conflictCode("IDEMPOTENCY_CONFLICT", "search ID reused with different evidence")
			}
			receipt.DurableRef = ref
			telemetry.Replay(ctx)
		}
		if err = tx.Commit(ctx); err != nil {
			return types.SearchCitationReceipt{}, err
		}
		telemetry.Add(ctx, map[string]any{"citation_commit": tag.RowsAffected() == 1, "evidence_count": len(pack.Evidence), "pack_hash": req.PackHash})
		return receipt, nil
	})
}

func citationEffectiveInTx(ctx context.Context, tx pgx.Tx, snapshot citationSnapshot, pointer int64) error {
	var valid bool
	err := tx.QueryRow(ctx, `SELECT m.data->>'lifecycle'='ENABLED'
 AND b.data->>'state'='READY' AND b.generation=$4
 AND b.data->>'manifest_hash'=r.data->>'manifest_hash'
 AND NOT EXISTS (
  SELECT 1 FROM (
   SELECT jsonb_array_elements_text(r.data->'source_revision_ids') AS id,'source' AS kind
   UNION ALL SELECT jsonb_array_elements_text(r.data->'wiki_revision_ids') AS id,'wiki' AS kind
  ) member LEFT JOIN knowledge_revisions v ON v.id=member.id AND v.module_id=r.module_id
  WHERE v.id IS NULL OR v.withdrawn OR v.kind<>member.kind
 )
 FROM knowledge_publications p
 JOIN knowledge_modules m ON m.id=p.module_id
 JOIN knowledge_releases r ON r.id=p.release_id AND r.module_id=p.module_id
 JOIN knowledge_builds b ON b.id=p.build_id AND b.release_id=p.release_id AND b.module_id=p.module_id
 WHERE p.module_id=$1 AND p.release_id=$2 AND p.pointer_revision=$3`,
		snapshot.ModuleID, snapshot.ReleaseID, pointer, snapshot.Generation).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !valid {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) citationReceipt(ctx context.Context, searchID string) (types.SearchCitationReceipt, string, error) {
	var receipt types.SearchCitationReceipt
	var raw string
	err := s.DB.QueryRow(ctx, "SELECT pack_hash,pack_json,durable_ref FROM knowledge_search_citations WHERE search_id=$1", searchID).
		Scan(&receipt.PackHash, &raw, &receipt.DurableRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return receipt, "", ErrNotFound
	}
	receipt.SearchId = searchID
	return receipt, raw, err
}

// GetSearchCitations is metadata-only. It never republishes quote text, and
// marks citations unavailable after a later withdrawal instead of swapping in
// a newer revision.
func (s *Store) GetSearchCitations(ctx context.Context, searchID string) (types.SearchCitationRecord, error) {
	return observe(ctx, s, "knowledge.search.citations.get", "search-citations:"+searchID, struct{ SearchId string }{searchID}, func(ctx context.Context) (types.SearchCitationRecord, error) {
		if !citationIdentity(searchID) {
			return types.SearchCitationRecord{}, invalid("search ID required")
		}
		receipt, raw, err := s.citationReceipt(ctx, searchID)
		if err != nil {
			return types.SearchCitationRecord{}, err
		}
		var pack citationPack
		if err = decodeCitationJSON([]byte(raw), &pack); err != nil || pack.SearchID != searchID {
			return types.SearchCitationRecord{}, ErrArtifactUnavailable
		}
		record := types.SearchCitationRecord{SearchId: searchID, PackHash: receipt.PackHash, DurableRef: receipt.DurableRef,
			ModuleId: pack.Snapshot.ModuleID, ReleaseId: pack.Snapshot.ReleaseID,
			Generation: pack.Snapshot.Generation, PublicationRevision: pack.Snapshot.PublicationRevision,
			Evidence: make([]types.SearchCitationReference, 0, len(pack.Evidence))}
		_, releaseErr := s.historicalRelease(ctx, pack.Snapshot.ModuleID, pack.Snapshot.ReleaseID)
		for _, evidence := range pack.Evidence {
			state := "available"
			if releaseErr != nil {
				state = "unavailable"
			}
			record.Evidence = append(record.Evidence, types.SearchCitationReference{EvidenceId: evidence.ID,
				SourceKind: evidence.Key.SourceKind, ContentId: evidence.Key.ContentID,
				RevisionId: evidence.Key.RevisionID, ChunkId: evidence.Key.ChunkID,
				Original: evidence.Original, Locator: evidence.Locator,
				QuoteHash: evidence.QuoteHash, State: state})
		}
		if releaseErr != nil && !errors.Is(releaseErr, ErrUnavailable) && !errors.Is(releaseErr, ErrNotFound) {
			return types.SearchCitationRecord{}, releaseErr
		}
		return record, nil
	})
}
