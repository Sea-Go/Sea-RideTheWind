package freeze

import (
	"reflect"
	"strings"
	"testing"

	"sea-try-go/service/knowledge/internal/structure"
)

func TestCanonicalEmptyTree(t *testing.T) {
	want := `{"revision_id":"rev-empty"}` + "\n"
	tree := &structure.Tree{RevisionID: "rev-empty"}
	if got := string(canonicalTreeJSON(tree)); got != want {
		t.Fatalf("canonical empty tree:\n got %q\nwant %q", got, want)
	}
	parsed, err := parseCanonicalTree([]byte(want))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(parsed, tree) {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", parsed, tree)
	}
}

func TestCanonicalEscaping(t *testing.T) {
	tree := &structure.Tree{
		RevisionID: "rev-\"esc\"\\",
		Nodes: []structure.Node{
			{NodeID: "n1", Level: 1, Title: "题\tTab \"q\" \\ ", ParaIndex: structure.HeadingAbsent, CharStart: 0, CharEnd: 20},
			{NodeID: "n2", Level: structure.LevelParagraph, Title: "", ParaIndex: 0, CharStart: 21, CharEnd: 40},
		},
	}
	canonical := canonicalTreeJSON(tree)
	parsed, err := parseCanonicalTree(canonical)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(parsed, tree) {
		t.Fatalf("roundtrip mismatch:\n got %+v\nwant %+v", parsed, tree)
	}
	// Every escaping the writer performs appears in the bytes.
	for _, sub := range []string{`\"`, `\\`, `\t`, "\n"} {
		if !strings.Contains(string(canonical), sub) {
			t.Fatalf("canonical bytes missing escape %q: %q", sub, canonical)
		}
	}
}

func TestParseCanonicalRejects(t *testing.T) {
	header := `{"revision_id":"rev-x"}` + "\n"
	node := `{"node_id":"n1","level":7,"title":"t","para_index":0,"char_start":1,"char_end":2}` + "\n"
	heading := `{"node_id":"h1","level":2,"title":"T","para_index":-1,"char_start":0,"char_end":5}` + "\n"
	cases := []struct {
		name string
		data string
		want string
	}{
		{"empty", "", "canonical header"},
		{"no trailing newline", strings.TrimSuffix(header, "\n"), "canonical header"},
		{"wrong header key order", `{"level":7,"revision_id":"x"}` + "\n", "canonical header"},
		{"unknown field", header + strings.Replace(node, `,"title":`, `,"titel":`, 1), "canonical node"},
		{"field order swapped", header + `{"level":7,"node_id":"n1","title":"t","para_index":0,"char_start":1,"char_end":2}` + "\n", "canonical node"},
		{"missing end quote", header + `{"node_id":"n1}` + "\n", "canonical node"},
		{"bad escape", `{"revision_id":"a\qb"}` + "\n", "canonical header"},
		{"raw control byte", "{\"revision_id\":\"a\nb\"}\n", "canonical header"},
		{"surrogate escape", `{"revision_id":"a` + `\ud800` + `"}` + "\n", "canonical header"},
		{"leading zero int", header + strings.Replace(node, `"level":7`, `"level":07`, 1), "leading zero"},
		{"negative zero", header + strings.Replace(node, `"level":7`, `"level":-0`, 1), "not canonical"},
		{"trailing junk", header + node + "junk", "canonical node"},
		{"space between records", header + " " + node, "canonical node"},
		{"level out of range", header + strings.Replace(node, `"level":7`, `"level":8`, 1), "level 8 outside"},
		{"zero level", header + strings.Replace(node, `"level":7`, `"level":0`, 1), "level 0 outside"},
		{"para below -1", header + strings.Replace(node, `"para_index":0`, `"para_index":-2`, 1), "para_index -2 below"},
		{"heading with para", header + strings.Replace(heading, `"para_index":-1`, `"para_index":3`, 1), "heading carries para_index"},
		{"inverted span", header + strings.Replace(node, `"char_start":1`, `"char_start":5`, 1), "invalid span"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCanonicalTree([]byte(tc.data))
			if err == nil {
				t.Fatalf("expected parse error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestParseCanonicalControlCharEscape(t *testing.T) {
	// \u0001 round-trips through the \u escape dialect.
	tree := &structure.Tree{RevisionID: "rev-ctl", Nodes: []structure.Node{
		{NodeID: "n", Level: structure.LevelParagraph, ParaIndex: 0, CharStart: 0, CharEnd: 3, Title: "a\x01b"},
	}}
	canonical := canonicalTreeJSON(tree)
	if !strings.Contains(string(canonical), `\u0001`) {
		t.Fatalf("control char not escaped: %q", canonical)
	}
	parsed, err := parseCanonicalTree(canonical)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(parsed, tree) {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", parsed, tree)
	}
}
