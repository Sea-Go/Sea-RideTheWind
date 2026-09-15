package knowledge

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeromicro/go-zero/rest/pathvar"
	"sea-try-go/service/knowledge/api/internal/types"
)

func TestFreezeWikiFactSetJSONRejectsCorruptNestedCommands(t *testing.T) {
	valid := `{"source_revisions":[{"revision_id":"revision-a","content_sha256":"sha-a"}],` +
		`"facts":[{"source_revision_id":"revision-a","locator":"paragraph:1",` +
		`"source_quote":"A","source_quote_sha256":"sha-quote","required":true}],` +
		`"facts_complete":true,"reason":"human full scope declaration","idempotency_key":"fact-set-key"}`
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"valid nested boolean and arrays", valid, true},
		{"duplicate top key", strings.Replace(valid, `"facts_complete":true`,
			`"facts_complete":true,"facts_complete":true`, 1), false},
		{"escaped duplicate key", strings.Replace(valid, `"facts_complete":true`,
			`"facts_complete":true,"\u0066acts_complete":true`, 1), false},
		{"duplicate source SHA", strings.Replace(valid, `"content_sha256":"sha-a"`,
			`"content_sha256":"sha-a","content_sha256":"sha-b"`, 1), false},
		{"unknown fact property", strings.Replace(valid, `"required":true`,
			`"required":true,"grade":"3"`, 1), false},
		{"numeric required", strings.Replace(valid, `"required":true`, `"required":1`, 1), false},
		{"null source revision", strings.Replace(valid, `"revision_id":"revision-a"`,
			`"revision_id":null`, 1), false},
		{"missing fact required", strings.Replace(valid, `,"required":true`, ``, 1), false},
		{"empty fact array", strings.Replace(valid,
			`"facts":[{"source_revision_id":"revision-a","locator":"paragraph:1","source_quote":"A","source_quote_sha256":"sha-quote","required":true}]`,
			`"facts":[]`, 1), false},
		{"trailing JSON", valid + ` {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/v1/knowledge/modules/module-a/wiki-pages/page-a/revisions/wiki-a/fact-sets",
				strings.NewReader(tc.body))
			r = pathvar.WithVars(r, map[string]string{"module_id": "module-a",
				"page_id": "page-a", "wiki_revision_id": "wiki-a"})
			r.Header.Set("Content-Type", "application/json")
			var req types.FreezeWikiFactSetReq
			err := parseFreezeWikiFactSet(r, &req)
			if tc.valid != (err == nil) {
				t.Fatalf("command JSON boundary: valid=%v err=%v", tc.valid, err)
			}
			if tc.valid && (len(req.SourceRevisions) != 1 || len(req.Facts) != 1 ||
				!req.FactsComplete || !req.Facts[0].Required) {
				t.Fatalf("typed source/fact boolean changed after strict JSON read: %+v", req)
			}
		})
	}
}
