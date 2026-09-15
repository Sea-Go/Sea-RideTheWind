package model

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"
)

const wikiFactSetRevisionSchema = "rtw.wiki.fact-set-revision.v1"
const wikiFactSetEventType = "knowledge.wiki.fact-set.frozen.v1"
const wikiFactSetDeclarationSource = "admin_jwt_allowlist_declaration"

// The complete flag is an administrator's explicit assertion about exactly
// this approved Source scope. It is not a machine proof that no Fact was
// omitted, nor a realtime UserCenter proof of the actor's canonical UID.
type wikiFactSetRevision struct {
	SchemaVersion         string                        `json:"schema_version"`
	FactSetID             string                        `json:"fact_set_id"`
	FactSetRevisionID     string                        `json:"fact_set_revision_id"`
	FactSetRevision       string                        `json:"fact_set_revision"`
	BaseFactSetRevisionID string                        `json:"base_fact_set_revision_id"`
	ModuleID              string                        `json:"module_id"`
	PageID                string                        `json:"page_id"`
	WikiRevisionID        string                        `json:"wiki_revision_id"`
	BaseWikiRevisionID    string                        `json:"base_wiki_revision_id"`
	WikiOriginKind        string                        `json:"wiki_origin_kind"`
	OriginCompileID       string                        `json:"origin_compile_id"`
	WikiContentSHA256     string                        `json:"wiki_content_sha256"`
	SourceScopeRevision   string                        `json:"source_scope_revision"`
	SourceRevisions       []types.FactSetSourceRevision `json:"source_revisions"`
	Facts                 []types.FactSetFact           `json:"facts"`
	FactsComplete         bool                          `json:"facts_complete"`
	DeclarationSource     string                        `json:"declaration_source"`
	ActorID               string                        `json:"actor_id"`
	Reason                string                        `json:"reason"`
	FrozenAt              string                        `json:"frozen_at"`
}

func wikiFactSetRecord(r wikiFactSetRevision, setSHA, eventID, rawSHA, jcsSHA string) types.WikiFactSetRecord {
	return types.WikiFactSetRecord{SchemaVersion: r.SchemaVersion,
		FactSetId: r.FactSetID, FactSetRevisionId: r.FactSetRevisionID,
		FactSetRevision: r.FactSetRevision, BaseFactSetRevisionId: r.BaseFactSetRevisionID,
		ModuleId: r.ModuleID, PageId: r.PageID, WikiRevisionId: r.WikiRevisionID,
		BaseWikiRevisionId: r.BaseWikiRevisionID, WikiOriginKind: r.WikiOriginKind,
		OriginCompileId: r.OriginCompileID, WikiContentSha256: r.WikiContentSHA256,
		SourceScopeRevision: r.SourceScopeRevision, SourceRevisions: r.SourceRevisions,
		Facts: r.Facts, FactsComplete: r.FactsComplete,
		DeclarationSource: r.DeclarationSource, ActorId: r.ActorID,
		Reason: r.Reason, FrozenAt: r.FrozenAt, FactSetJcsSha256: setSHA,
		EventId: eventID, EventRawSha256: rawSHA, EventJcsSha256: jcsSHA}
}

func wikiFactSetScopeRevision(moduleID, pageID string, sources []types.FactSetSourceRevision) (string, error) {
	claim := struct {
		ModuleID        string                        `json:"module_id"`
		PageID          string                        `json:"page_id"`
		SourceRevisions []types.FactSetSourceRevision `json:"source_revisions"`
	}{moduleID, pageID, sources}
	raw, err := json.Marshal(claim)
	if err != nil {
		return "", err
	}
	h, err := wikiQualityJCSHash(raw)
	if err != nil {
		return "", err
	}
	return "scope_" + h, nil
}

func validWikiFactSetScope(scope string) bool {
	return strings.HasPrefix(scope, "scope_") && citationHash(strings.TrimPrefix(scope, "scope_"))
}

func validWikiFactSetConflictGroup(group string) bool {
	if group == "" {
		return true
	}
	if len(group) > 64 || len(group) < 1 {
		return false
	}
	for _, r := range group {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func validateFreezeWikiFactSet(actor string, req types.FreezeWikiFactSetReq) error {
	if !validLogicalSessionID(actor) || !citationIdentity(req.ModuleId) ||
		!qualityPageID(req.PageId) || !citationIdentity(req.WikiRevisionId) ||
		(req.OriginCompileId != "" && !citationIdentity(req.OriginCompileId)) ||
		(req.BaseFactSetRevisionId != "" && !citationIdentity(req.BaseFactSetRevisionId)) ||
		!citationIdentity(req.IdempotencyKey) || len(req.IdempotencyKey) < 8 ||
		len(req.IdempotencyKey) > 200 || !req.FactsComplete ||
		!utf8.ValidString(req.Reason) || len(req.Reason) < 1 || len(req.Reason) > 2000 ||
		strings.TrimSpace(req.Reason) == "" || strings.ContainsRune(req.Reason, 0) ||
		len(req.SourceRevisions) < 1 || len(req.SourceRevisions) > 64 ||
		len(req.Facts) < 1 || len(req.Facts) > 128 {
		return invalid("bounded complete administrator Wiki FactSet declaration required")
	}
	lastSource := ""
	for _, source := range req.SourceRevisions {
		if !citationIdentity(source.RevisionId) || !citationHash(source.ContentSha256) ||
			(lastSource != "" && source.RevisionId <= lastSource) {
			return invalid("Source scope revisions must be unique and in canonical order")
		}
		lastSource = source.RevisionId
	}
	for _, fact := range req.Facts {
		if !citationIdentity(fact.SourceRevisionId) || !validQualityLocator(fact.Locator) ||
			!citationHash(fact.SourceQuoteSha256) || !utf8.ValidString(fact.SourceQuote) ||
			len(fact.SourceQuote) < 1 || len(fact.SourceQuote) > 4096 ||
			strings.TrimSpace(fact.SourceQuote) == "" || strings.ContainsRune(fact.SourceQuote, 0) ||
			object.Hash([]byte(fact.SourceQuote)) != fact.SourceQuoteSha256 ||
			!validWikiFactSetConflictGroup(fact.ConflictGroup) {
			return invalid("Fact must name original bounded Source quote/paragraph/group")
		}
	}
	return nil
}

func validateFrozenWikiFactSet(r wikiFactSetRevision, setSHA string) error {
	if r.SchemaVersion != wikiFactSetRevisionSchema ||
		r.DeclarationSource != wikiFactSetDeclarationSource || !r.FactsComplete ||
		!citationIdentity(r.FactSetID) || !citationIdentity(r.FactSetRevisionID) ||
		!citationIdentity(r.ModuleID) || !qualityPageID(r.PageID) ||
		!citationIdentity(r.WikiRevisionID) || !citationHash(r.WikiContentSHA256) ||
		!validWikiFactSetScope(r.SourceScopeRevision) || !validLogicalSessionID(r.ActorID) ||
		!citationHash(setSHA) || len(r.SourceRevisions) < 1 || len(r.SourceRevisions) > 64 ||
		len(r.Facts) < 1 || len(r.Facts) > 128 ||
		(r.BaseFactSetRevisionID != "" && !citationIdentity(r.BaseFactSetRevisionID)) ||
		!utf8.ValidString(r.Reason) || len(r.Reason) < 1 || len(r.Reason) > 2000 ||
		strings.TrimSpace(r.Reason) == "" || strings.ContainsRune(r.Reason, 0) ||
		(r.WikiOriginKind != "ai_accepted" && r.WikiOriginKind != "manual_revision") ||
		(r.WikiOriginKind == "manual_revision" && r.OriginCompileID != "") ||
		(r.WikiOriginKind == "ai_accepted" && !citationIdentity(r.OriginCompileID)) {
		return ErrArtifactUnavailable
	}
	if frozenAt, err := time.Parse(time.RFC3339Nano, r.FrozenAt); err != nil ||
		frozenAt.IsZero() || frozenAt.Location() != time.UTC {
		return ErrArtifactUnavailable
	}
	seq, err := strconv.ParseInt(r.FactSetRevision, 10, 64)
	if err != nil || seq < 1 || r.FactSetRevision != strconv.FormatInt(seq, 10) {
		return ErrArtifactUnavailable
	}
	scope, err := wikiFactSetScopeRevision(r.ModuleID, r.PageID, r.SourceRevisions)
	if err != nil || scope != r.SourceScopeRevision {
		return ErrArtifactUnavailable
	}
	lastSource := ""
	known := map[string]string{}
	for _, source := range r.SourceRevisions {
		if !citationIdentity(source.RevisionId) || !citationHash(source.ContentSha256) ||
			(lastSource != "" && source.RevisionId <= lastSource) {
			return ErrArtifactUnavailable
		}
		known[source.RevisionId] = source.ContentSha256
		lastSource = source.RevisionId
	}
	lastFact := ""
	groups := map[string]int{}
	requiredCount := 0
	for _, fact := range r.Facts {
		if !validWikiFactID(fact.FactId) || fact.FactId != wikiQualityFactID(
			fact.SourceRevisionId, fact.Locator, fact.SourceQuoteSha256) ||
			fact.FactId <= lastFact || known[fact.SourceRevisionId] != fact.SourceContentSha256 ||
			!validQualityLocator(fact.Locator) || !citationHash(fact.SourceQuoteSha256) ||
			!utf8.ValidString(fact.SourceQuote) || len(fact.SourceQuote) < 1 ||
			len(fact.SourceQuote) > 4096 || strings.TrimSpace(fact.SourceQuote) == "" ||
			strings.ContainsRune(fact.SourceQuote, 0) ||
			object.Hash([]byte(fact.SourceQuote)) != fact.SourceQuoteSha256 ||
			!validWikiFactSetConflictGroup(fact.ConflictGroup) {
			return ErrArtifactUnavailable
		}
		start, startErr := strconv.ParseInt(fact.SourceByteStart, 10, 64)
		end, endErr := strconv.ParseInt(fact.SourceByteEnd, 10, 64)
		if startErr != nil || endErr != nil || start < 0 || end <= start ||
			fact.SourceByteStart != strconv.FormatInt(start, 10) ||
			fact.SourceByteEnd != strconv.FormatInt(end, 10) || end-start != int64(len(fact.SourceQuote)) {
			return ErrArtifactUnavailable
		}
		if fact.ConflictGroup != "" {
			groups[fact.ConflictGroup]++
		}
		if fact.Required {
			requiredCount++
		}
		lastFact = fact.FactId
	}
	if requiredCount < 1 {
		return ErrArtifactUnavailable
	}
	for _, n := range groups {
		if n < 2 {
			return ErrArtifactUnavailable
		}
	}
	return nil
}

func sortWikiFactSetFacts(facts []types.FactSetFact) {
	sort.Slice(facts, func(i, j int) bool { return facts[i].FactId < facts[j].FactId })
}
