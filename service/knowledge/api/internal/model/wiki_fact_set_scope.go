package model

import (
	"bytes"
	"context"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

type wikiFactSetTarget struct {
	wiki                types.Revision
	originKind          string
	originCompileID     string
	sourceScopeRevision string
	sources             []types.FactSetSourceRevision
	facts               []types.FactSetFact
}

// AI scope is the entire Source set frozen by its accepted Compile. A manual
// Wiki scope is the union of its own and same-page base-chain citations plus
// any accepted Compile's full frozen set in that chain. A submitted subset
// never qualifies as a declaration that the page's expected Facts are full.
func wikiFactSetApprovedSourceIDs(ctx context.Context, tx pgx.Tx,
	wiki types.Revision, requestedCompileID string) ([]string, string, string, error) {
	if strings.HasPrefix(wiki.CreatedBy, "btw.compile/") {
		compileID := strings.TrimPrefix(wiki.CreatedBy, "btw.compile/")
		if !citationIdentity(compileID) ||
			(requestedCompileID != "" && requestedCompileID != compileID) {
			return nil, "", "", invalid("AI Wiki Compile differs from original provenance")
		}
		compile, err := readJSON[types.Compile](ctx, tx,
			"SELECT data FROM knowledge_compiles WHERE id=$1 AND module_id=$2 AND page_id=$3",
			compileID, wiki.ModuleId, wiki.EntityId)
		if err != nil || compile.State != "ACCEPTED" || compile.RevisionId != wiki.RevisionId ||
			compile.ModuleId != wiki.ModuleId || compile.PageId != wiki.EntityId ||
			len(compile.SourceRevisionIds) < 1 || len(compile.SourceRevisionIds) > 64 {
			return nil, "", "", ErrArtifactUnavailable
		}
		ids := append([]string(nil), compile.SourceRevisionIds...)
		for i, id := range ids {
			if !citationIdentity(id) || i > 0 && id <= ids[i-1] {
				return nil, "", "", ErrArtifactUnavailable
			}
		}
		return ids, "ai_accepted", compileID, nil
	}
	if requestedCompileID != "" || wiki.CreatedBy == "" ||
		len(wiki.SourceRefs) < 1 || len(wiki.SourceRefs) > 64 {
		return nil, "", "", invalid("manual Wiki FactSet needs its own actor and source lineage")
	}
	currentRefs := make(map[string]bool, len(wiki.SourceRefs))
	for _, ref := range wiki.SourceRefs {
		currentRefs[ref.RevisionId] = true
	}
	moduleID := wiki.ModuleId
	set := map[string]bool{}
	seen := map[string]bool{}
	for depth := 0; depth < 64; depth++ {
		if seen[wiki.RevisionId] || wiki.Kind != "wiki" {
			return nil, "", "", ErrArtifactUnavailable
		}
		seen[wiki.RevisionId] = true
		if len(wiki.SourceRefs) > 64 {
			return nil, "", "", ErrArtifactUnavailable
		}
		for _, ref := range wiki.SourceRefs {
			if !citationIdentity(ref.RevisionId) || !validQualityLocator(ref.Locator) {
				return nil, "", "", ErrArtifactUnavailable
			}
			set[ref.RevisionId] = true
		}
		if strings.HasPrefix(wiki.CreatedBy, "btw.compile/") {
			ids, _, _, err := wikiFactSetApprovedSourceIDs(ctx, tx, wiki, "")
			if err != nil {
				return nil, "", "", err
			}
			for _, id := range ids {
				set[id] = true
			}
		}
		if len(set) > 64 {
			return nil, "", "", conflict("manual page Source scope exceeds v1 bound")
		}
		if wiki.BaseRevisionId == "" {
			ids, err := manualWikiFactSetCurrentSourceIDs(ctx, tx, moduleID, set, currentRefs)
			return ids, "manual_revision", "", err
		}
		base, err := revision(ctx, tx, wiki.BaseRevisionId)
		if err != nil || base.Kind != "wiki" || base.ModuleId != wiki.ModuleId ||
			base.EntityId != wiki.EntityId {
			return nil, "", "", ErrArtifactUnavailable
		}
		wiki = base
	}
	return nil, "", "", conflict("manual Wiki Source lineage exceeds v1 depth")
}

// An RTW withdrawal is the explicit business act that retires an old Source
// from a *new* manual scope. Never silently trim an available ancestor or a
// current Wiki citation. Historical FactSet revisions keep their old scope.
func manualWikiFactSetCurrentSourceIDs(ctx context.Context, tx pgx.Tx,
	moduleID string, lineage, currentRefs map[string]bool) ([]string, error) {
	ids := make([]string, 0, len(lineage))
	for id := range lineage {
		source, err := revision(ctx, tx, id)
		if err != nil || source.Kind != "source" || source.ModuleId != moduleID {
			return nil, ErrArtifactUnavailable
		}
		if source.Withdrawn {
			if currentRefs[id] {
				return nil, conflict("current manual Wiki cites a retired Source")
			}
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) < 1 {
		return nil, conflict("manual Wiki lineage has no available Source Fact scope")
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *Store) wikiFactSetTarget(ctx context.Context, tx pgx.Tx,
	req types.FreezeWikiFactSetReq) (wikiFactSetTarget, error) {
	var target wikiFactSetTarget
	wiki, err := revision(ctx, tx, req.WikiRevisionId)
	if err != nil {
		return target, err
	}
	if wiki.Kind != "wiki" || wiki.ModuleId != req.ModuleId || wiki.EntityId != req.PageId ||
		wiki.Withdrawn || !citationHash(wiki.ContentHash) ||
		wiki.ObjectKey != object.Key(wiki.ContentHash) {
		return target, invalid("FactSet needs an available immutable Wiki page revision")
	}
	wikiBytes, err := s.Objects.Get(ctx, wiki.ObjectKey, wiki.ContentHash)
	if err != nil || object.Hash(wikiBytes) != wiki.ContentHash || !utf8.Valid(wikiBytes) {
		return target, ErrArtifactUnavailable
	}
	approvedIDs, originKind, compileID, err := wikiFactSetApprovedSourceIDs(
		ctx, tx, wiki, req.OriginCompileId)
	if err != nil {
		return target, err
	}
	if len(approvedIDs) != len(req.SourceRevisions) {
		return target, invalid("FactSet Source list is a subset or superset of approved Wiki scope")
	}
	target.wiki, target.originKind, target.originCompileID = wiki, originKind, compileID
	target.sources = make([]types.FactSetSourceRevision, 0, len(approvedIDs))
	sourceBytes := make(map[string][]byte, len(approvedIDs))
	sourceSHA := make(map[string]string, len(approvedIDs))
	var totalBytes int64
	for i, id := range approvedIDs {
		declared := req.SourceRevisions[i]
		if declared.RevisionId != id {
			return target, invalid("FactSet Source order differs from complete approved scope")
		}
		source, err := revision(ctx, tx, id)
		if err != nil || source.Kind != "source" || source.ModuleId != req.ModuleId ||
			source.Withdrawn || !citationHash(source.ContentHash) ||
			source.ContentHash != declared.ContentSha256 ||
			source.ObjectKey != object.Key(source.ContentHash) ||
			(source.MediaType != "text/plain" && source.MediaType != "text/markdown") {
			return target, invalid("FactSet Source SHA/identity is not an available approved text revision")
		}
		original, err := s.Objects.Get(ctx, source.ObjectKey, source.ContentHash)
		if err != nil || object.Hash(original) != source.ContentHash ||
			!utf8.Valid(original) || bytes.IndexByte(original, 0) >= 0 {
			return target, ErrArtifactUnavailable
		}
		totalBytes += int64(len(original))
		if totalBytes > 64<<20 {
			return target, invalid("combined original FactSet Source bytes exceed v1 bound")
		}
		target.sources = append(target.sources, declared)
		sourceBytes[id] = original
		sourceSHA[id] = declared.ContentSha256
	}
	scope, err := wikiFactSetScopeRevision(req.ModuleId, req.PageId, target.sources)
	if err != nil {
		return target, err
	}
	target.sourceScopeRevision = scope
	seenFacts := map[string]bool{}
	groups := map[string]int{}
	var requiredCount int
	paragraphs := map[string][2]int{}
	for _, proposed := range req.Facts {
		original, ok := sourceBytes[proposed.SourceRevisionId]
		if !ok {
			return target, invalid("Fact uses a Source outside the full approved scope")
		}
		paragraphKey := proposed.SourceRevisionId + "\x00" + proposed.Locator
		span, cached := paragraphs[paragraphKey]
		if !cached {
			_, start, end, valid := citationParagraph(string(original), proposed.Locator)
			if !valid || start < 0 || end <= start || end > len(original) {
				return target, invalid("Fact locator is absent from original Source bytes")
			}
			span = [2]int{start, end}
			paragraphs[paragraphKey] = span
		}
		quoteOffset := bytes.Index(original[span[0]:span[1]], []byte(proposed.SourceQuote))
		if quoteOffset < 0 {
			return target, invalid("Fact quote is absent from original Source paragraph bytes")
		}
		factID := wikiQualityFactID(proposed.SourceRevisionId,
			proposed.Locator, proposed.SourceQuoteSha256)
		if seenFacts[factID] {
			return target, invalid("duplicate FactID in one completeness declaration")
		}
		seenFacts[factID] = true
		if proposed.Required {
			requiredCount++
		}
		if proposed.ConflictGroup != "" {
			groups[proposed.ConflictGroup]++
		}
		start := span[0] + quoteOffset
		fact := types.FactSetFact{FactId: factID,
			SourceRevisionId:    proposed.SourceRevisionId,
			SourceContentSha256: sourceSHA[proposed.SourceRevisionId],
			Locator:             proposed.Locator, SourceByteStart: strconv.Itoa(start),
			SourceByteEnd: strconv.Itoa(start + len(proposed.SourceQuote)),
			SourceQuote:   proposed.SourceQuote, SourceQuoteSha256: proposed.SourceQuoteSha256,
			Required: proposed.Required, ConflictGroup: proposed.ConflictGroup}
		target.facts = append(target.facts, fact)
	}
	if requiredCount < 1 {
		return target, invalid("complete expected FactSet must mark at least one Fact required")
	}
	for _, count := range groups {
		if count < 2 {
			return target, invalid("conflict group must name at least two distinct Source Facts")
		}
	}
	sortWikiFactSetFacts(target.facts)
	return target, nil
}
