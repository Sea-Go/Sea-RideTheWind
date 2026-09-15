package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode/utf8"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

const wikiQualityRevisionSchema = "rtw.wiki.quality-judgment.v1"
const wikiQualityRubric = "sea.wiki.fact-coverage.v1"
const wikiQualityEventType = "knowledge.wiki.quality.judged.v1"
const wikiQualityJudgmentSource = "human_admin_jwt_allowlist"

// A fact grade is human coverage/citation quality, not confidence or a D07
// release threshold. Missing/conflict are 0; covered is 1..3; undetermined
// has no numeric grade and cannot be conflated with missing.
type wikiQualityRevision struct {
	SchemaVersion       string `json:"schema_version"`
	JudgmentSource      string `json:"judgment_source"`
	JudgmentID          string `json:"judgment_id"`
	FactID              string `json:"fact_id"`
	JudgeRevisionID     string `json:"judge_revision_id"`
	JudgeRevision       string `json:"judge_revision"`
	BaseJudgeRevisionID string `json:"base_judge_revision_id"`
	ModuleID            string `json:"module_id"`
	PageID              string `json:"page_id"`
	WikiRevisionID      string `json:"wiki_revision_id"`
	BaseWikiRevisionID  string `json:"base_wiki_revision_id"`
	WikiOriginKind      string `json:"wiki_origin_kind"`
	OriginCompileID     string `json:"origin_compile_id"`
	WikiContentSHA256   string `json:"wiki_content_sha256"`
	SourceRevisionID    string `json:"source_revision_id"`
	SourceContentSHA256 string `json:"source_content_sha256"`
	Locator             string `json:"locator"`
	SourceByteStart     string `json:"source_byte_start"`
	SourceByteEnd       string `json:"source_byte_end"`
	SourceQuote         string `json:"source_quote"`
	SourceQuoteSHA256   string `json:"source_quote_sha256"`
	WikiClaimText       string `json:"wiki_claim_text"`
	WikiClaimSHA256     string `json:"wiki_claim_sha256"`
	CitationPresent     bool   `json:"citation_present"`
	Assessment          string `json:"assessment"`
	Grade               *int   `json:"grade"`
	RubricVersion       string `json:"rubric_version"`
	Reason              string `json:"reason"`
	ActorID             string `json:"actor_id"`
	JudgedAt            string `json:"judged_at"`
	SourceWithdrawn     bool   `json:"source_withdrawn"`
	WikiWithdrawn       bool   `json:"wiki_withdrawn"`
}

func wikiQualityJCSHash(raw []byte) (string, error) {
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func wikiQualityFactID(sourceID, locator, quoteSHA string) string {
	return "fact_" + object.Hash([]byte(sourceID+"\x00"+locator+"\x00"+quoteSHA))
}

func validWikiFactID(factID string) bool {
	return strings.HasPrefix(factID, "fact_") && citationHash(strings.TrimPrefix(factID, "fact_"))
}

func qualityPageID(id string) bool {
	return id != "" && len(id) <= 200 && utf8.ValidString(id) &&
		strings.TrimSpace(id) != "" && !strings.ContainsRune(id, 0)
}

func wikiQualityGrade(assessment, raw string) (*int, error) {
	if assessment == "undetermined" {
		if raw != "" {
			return nil, invalid("undetermined fact has no coverage grade")
		}
		return nil, nil
	}
	grade, err := strconv.Atoi(raw)
	if err != nil || strconv.Itoa(grade) != raw {
		return nil, invalid("canonical coverage grade required")
	}
	if (assessment == "covered" && grade >= 1 && grade <= 3) ||
		((assessment == "missing" || assessment == "conflict") && grade == 0) {
		return &grade, nil
	}
	return nil, invalid("fact category and coverage grade differ")
}

func validateWikiQualityRequest(actor string, req types.JudgeWikiFactReq) (*int, error) {
	if !validLogicalSessionID(actor) || !citationIdentity(req.ModuleId) ||
		!qualityPageID(req.PageId) || !citationIdentity(req.WikiRevisionId) ||
		!citationIdentity(req.SourceRevisionId) || !citationHash(req.SourceContentSha256) ||
		!citationHash(req.SourceQuoteSha256) || req.RubricVersion != wikiQualityRubric ||
		!citationIdentity(req.IdempotencyKey) || len(req.IdempotencyKey) < 8 ||
		len(req.IdempotencyKey) > 200 || (req.OriginCompileId != "" && !citationIdentity(req.OriginCompileId)) ||
		!utf8.ValidString(req.SourceQuote) || len(req.SourceQuote) < 1 || len(req.SourceQuote) > 4096 ||
		strings.TrimSpace(req.SourceQuote) == "" || strings.ContainsRune(req.SourceQuote, 0) ||
		!utf8.ValidString(req.Reason) || len(req.Reason) < 1 || len(req.Reason) > 2000 ||
		strings.TrimSpace(req.Reason) == "" || strings.ContainsRune(req.Reason, 0) ||
		(req.BaseJudgeRevisionId != "" && !citationIdentity(req.BaseJudgeRevisionId)) ||
		!validQualityLocator(req.Locator) {
		return nil, invalid("bounded Wiki/source/fact/rubric/administrator judgment required")
	}
	if object.Hash([]byte(req.SourceQuote)) != req.SourceQuoteSha256 {
		return nil, invalid("source quote SHA differs from original quote bytes")
	}
	if req.WikiClaimText == "" {
		if req.WikiClaimSha256 != "" || req.Assessment == "covered" || req.Assessment == "conflict" {
			return nil, invalid("covered or conflicting fact requires a real Wiki claim")
		}
	} else if !utf8.ValidString(req.WikiClaimText) || len(req.WikiClaimText) > 4096 ||
		strings.ContainsRune(req.WikiClaimText, 0) ||
		!citationHash(req.WikiClaimSha256) || object.Hash([]byte(req.WikiClaimText)) != req.WikiClaimSha256 ||
		req.Assessment == "missing" {
		return nil, invalid("Wiki claim must be an original bounded byte span")
	}
	return wikiQualityGrade(req.Assessment, req.Grade)
}

func validQualityLocator(locator string) bool {
	if !strings.HasPrefix(locator, "paragraph:") {
		return false
	}
	ordinal, err := strconv.ParseInt(strings.TrimPrefix(locator, "paragraph:"), 10, 64)
	return err == nil && ordinal > 0 && locator == "paragraph:"+strconv.FormatInt(ordinal, 10)
}
