package main

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"k8s.io/kube-openapi/pkg/validation/spec"
	"k8s.io/kube-openapi/pkg/validation/strfmt"
	"k8s.io/kube-openapi/pkg/validation/validate"
)

type generatedHTTPContract struct {
	Paths map[string]map[string]struct {
		Parameters []struct {
			Name     string `json:"name"`
			In       string `json:"in"`
			Required bool   `json:"required"`
			Default  any    `json:"default"`
		} `json:"parameters"`
		Responses map[string]struct {
			Schema spec.Schema `json:"schema"`
		} `json:"responses"`
	} `json:"paths"`
}

func loadGeneratedHTTPContract(t *testing.T) generatedHTTPContract {
	t.Helper()
	raw, err := os.ReadFile("../generated/knowledge.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract generatedHTTPContract
	if err = json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	return contract
}

// validateSuccess checks exact bytes captured from the live go-zero process,
// rather than rebuilding a fixture from Go response structs. The existing
// pinned kube-openapi dependency provides the Swagger Draft 4 schema validator.
func (c generatedHTTPContract) validateSuccess(t *testing.T, method string, requestURL *url.URL, body []byte) {
	t.Helper()
	path := strings.Split(requestURL.Path, "/")
	matched := ""
	static := -1
	for template, methods := range c.Paths {
		if _, ok := methods[strings.ToLower(method)]; !ok {
			continue
		}
		parts := strings.Split(template, "/")
		if len(parts) != len(path) {
			continue
		}
		score := 0
		matches := true
		for i, part := range parts {
			if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
				continue
			}
			if part != path[i] {
				matches = false
				break
			}
			score++
		}
		if matches && score > static {
			matched, static = template, score
		}
	}
	if matched == "" {
		t.Fatalf("generated contract has no %s %s", method, requestURL.Path)
	}
	op := c.Paths[matched][strings.ToLower(method)]
	for _, p := range op.Parameters {
		if p.In == "query" && p.Required && !requestURL.Query().Has(p.Name) {
			t.Fatalf("HTTP accepted omitted %s but generated %s requires it", p.Name, matched)
		}
	}
	response, ok := op.Responses["200"]
	if !ok {
		t.Fatalf("generated contract has no 200 response for %s", matched)
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	if err := validate.AgainstSchema(&response.Schema, value, strfmt.Default); err != nil {
		t.Fatalf("live %s %s response violates generated Swagger: %v\n%s", method, requestURL.RequestURI(), err, body)
	}
}

func TestGeneratedReaderOptionality(t *testing.T) {
	contract := loadGeneratedHTTPContract(t)
	paths := []string{
		"/v1/knowledge/modules",
		"/v1/knowledge/workbench/modules",
		"/v1/knowledge/modules/{module_id}/revisions",
		"/v1/knowledge/modules/{module_id}/releases",
		"/v1/knowledge/modules/{module_id}/builds",
		"/v1/knowledge/modules/{module_id}/compiles",
		"/v1/knowledge/modules/{module_id}/releases/{release_id}/revisions",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			op := contract.Paths[path]["get"]
			limitFound := false
			wantDefault := float64(20)
			if path == paths[0] || path == paths[1] {
				wantDefault = 12
			}
			for _, p := range op.Parameters {
				if p.In == "query" && p.Name == "limit" {
					limitFound = true
					if p.Required || p.Default != wantDefault {
						t.Errorf("limit required=%v default=%v want optional default %v", p.Required, p.Default, wantDefault)
					}
				}
			}
			if !limitFound {
				t.Fatal("limit missing from contract")
			}
			data := op.Responses["200"].Schema.Properties["data"]
			for _, field := range data.Required {
				if field == "next_cursor" {
					t.Error("final page next_cursor must be optional")
				}
			}
			if strings.HasSuffix(path, "/revisions") {
				item := data.Properties["items"].Items.Schema
				if item == nil {
					t.Fatal("missing Revision item schema")
				}
				for _, field := range item.Required {
					if field == "content" {
						t.Error("metadata-only list content must be optional")
					}
				}
			}
		})
	}
}
