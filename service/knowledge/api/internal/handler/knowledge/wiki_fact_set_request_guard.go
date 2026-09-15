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

// Validate literal duplicate/unknown JSON fields and all nested types before
// go-zero's typed parser can discard an extra source/fact or one declaration.
func parseFreezeWikiFactSet(r *http.Request, req *types.FreezeWikiFactSetReq) error {
	if r == nil || r.Body == nil {
		return model.ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 768<<10+1))
	if err != nil || len(raw) < 2 || len(raw) > 768<<10 {
		return model.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := wikiFactSetJSONFields(d, map[string]byte{
		"origin_compile_id": 's', "source_revisions": 'S', "facts": 'F',
		"facts_complete": 'b', "reason": 's',
		"base_fact_set_revision_id": 's', "idempotency_key": 's',
	}, map[string]bool{"source_revisions": true, "facts": true,
		"facts_complete": true, "reason": true, "idempotency_key": true}); err != nil {
		return model.ErrInvalid
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return model.ErrInvalid
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if err := httpx.Parse(r, req); err != nil {
		return model.ErrInvalid
	}
	return nil
}

func wikiFactSetJSONFields(d *json.Decoder, allowed map[string]byte,
	required map[string]bool) error {
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return model.ErrInvalid
	}
	seen := make(map[string]bool, len(allowed))
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		kind, allowedKey := allowed[name]
		if err != nil || !ok || !allowedKey || seen[name] {
			return model.ErrInvalid
		}
		seen[name] = true
		switch kind {
		case 's', 'b':
			v, err := d.Token()
			if err != nil || kind == 's' && !wikiFactSetJSONString(v) ||
				kind == 'b' && !wikiFactSetJSONBool(v) {
				return model.ErrInvalid
			}
		case 'S', 'F':
			if err := wikiFactSetJSONArray(d, kind); err != nil {
				return err
			}
		default:
			return model.ErrInvalid
		}
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') {
		return model.ErrInvalid
	}
	for name := range required {
		if !seen[name] {
			return model.ErrInvalid
		}
	}
	return nil
}

func wikiFactSetJSONArray(d *json.Decoder, kind byte) error {
	start, err := d.Token()
	if err != nil || start != json.Delim('[') {
		return model.ErrInvalid
	}
	var allowed map[string]byte
	var required map[string]bool
	max := 64
	if kind == 'S' {
		allowed = map[string]byte{"revision_id": 's', "content_sha256": 's'}
		required = map[string]bool{"revision_id": true, "content_sha256": true}
	} else {
		max = 128
		allowed = map[string]byte{"source_revision_id": 's', "locator": 's',
			"source_quote": 's', "source_quote_sha256": 's',
			"required": 'b', "conflict_group": 's'}
		required = map[string]bool{"source_revision_id": true, "locator": true,
			"source_quote": true, "source_quote_sha256": true, "required": true}
	}
	count := 0
	for d.More() {
		count++
		if count > max || wikiFactSetJSONFields(d, allowed, required) != nil {
			return model.ErrInvalid
		}
	}
	end, err := d.Token()
	if err != nil || end != json.Delim(']') || count < 1 {
		return model.ErrInvalid
	}
	return nil
}

func wikiFactSetJSONString(v any) bool { _, ok := v.(string); return ok }
func wikiFactSetJSONBool(v any) bool   { _, ok := v.(bool); return ok }
