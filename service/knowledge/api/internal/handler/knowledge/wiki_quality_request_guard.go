package knowledge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// Human fact labels are flat, bounded string commands. Reject duplicate or
// unknown literal keys before go-zero's typed parser can discard one value.
func parseJudgeWikiFact(r *http.Request, req *types.JudgeWikiFactReq) error {
	if r == nil || r.Body == nil {
		return model.ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<10+1))
	if err != nil || len(raw) < 2 || len(raw) > 32<<10 {
		return model.ErrInvalid
	}
	allowed := map[string]bool{
		"origin_compile_id": true, "source_revision_id": true,
		"source_content_sha256": true, "locator": true, "source_quote": true,
		"source_quote_sha256": true, "wiki_claim_text": true,
		"wiki_claim_sha256": true, "assessment": true, "grade": true,
		"rubric_version": true, "reason": true,
		"base_judge_revision_id": true, "idempotency_key": true,
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return model.ErrInvalid
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || !allowed[name] || seen[name] {
			return model.ErrInvalid
		}
		value, err := decoder.Token()
		if err != nil {
			return model.ErrInvalid
		}
		if _, ok := value.(string); !ok {
			return model.ErrInvalid
		}
		seen[name] = true
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return model.ErrInvalid
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return model.ErrInvalid
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if err := httpx.Parse(r, req); err != nil {
		return model.ErrInvalid
	}
	return nil
}
