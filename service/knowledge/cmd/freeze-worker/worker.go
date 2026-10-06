package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"sea-try-go/service/knowledge/internal/citation"
	"sea-try-go/service/knowledge/internal/freeze"
	"sea-try-go/service/knowledge/internal/structure"
)

type workerOptions struct {
	input  string
	accept bool
}

func parseFlags(args []string, stderr io.Writer) (workerOptions, error) {
	fs := flag.NewFlagSet("freeze-worker", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts workerOptions
	fs.StringVar(&opts.input, "input", "", "JSONL file with release requests ('-' reads stdin)")
	fs.BoolVar(&opts.accept, "accept", false, "demo citation acceptance with a sample locator quote after releasing")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.input == "" || fs.NArg() != 0 {
		return opts, fmt.Errorf("exactly --input is required")
	}
	return opts, nil
}

func openInput(path string) (io.Reader, func(), error) {
	if path == "-" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}

// releaseRequest is one JSONL line: the release envelope plus its docs.
type releaseRequest struct {
	ModuleID    string       `json:"module_id"`
	ReleaseID   string       `json:"release_id"`
	PublishedAt string       `json:"published_at,omitempty"`
	Docs        []docRequest `json:"docs"`
}

type docRequest struct {
	RevisionID    string `json:"revision_id"`
	DocKey        string `json:"doc_key"`
	ContentSHA256 string `json:"content_sha256,omitempty"`
	SourceB64     string `json:"source_b64"`
}

func readRequests(r io.Reader) ([]releaseRequest, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var out []releaseRequest
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var req releaseRequest
		dec := json.NewDecoder(strings.NewReader(text))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		out = append(out, req)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// toFrozen converts a docRequest into a FrozenRevision, computing the
// content sha when omitted and rejecting a stale one.
func (d docRequest) toFrozen(module string) (freeze.FrozenRevision, error) {
	source, err := base64.StdEncoding.DecodeString(d.SourceB64)
	if err != nil {
		return freeze.FrozenRevision{}, fmt.Errorf("doc %s: source_b64: %w", d.RevisionID, err)
	}
	sum := sha256.Sum256(source)
	computed := hex.EncodeToString(sum[:])
	if d.ContentSHA256 == "" {
		d.ContentSHA256 = computed
	} else if d.ContentSHA256 != computed {
		return freeze.FrozenRevision{}, fmt.Errorf("doc %s: content_sha256 %s does not match source bytes (%s)",
			d.RevisionID, d.ContentSHA256, computed)
	}
	return freeze.FrozenRevision{
		RevisionID:    d.RevisionID,
		ModuleID:      module,
		DocKey:        d.DocKey,
		Source:        source,
		ContentSHA256: d.ContentSHA256,
	}, nil
}

type frozenDocOut struct {
	fr      freeze.FrozenRevision
	treeSHA string
}

type releaseOut struct {
	req     releaseRequest
	at      time.Time
	eventID string
	docs    []frozenDocOut
}

func newService() *freeze.Service {
	return freeze.NewMemoryService()
}

func processRequest(ctx context.Context, svc *freeze.Service, req releaseRequest) (releaseOut, error) {
	out := releaseOut{req: req}
	if req.PublishedAt != "" {
		at, err := time.Parse(time.RFC3339, req.PublishedAt)
		if err != nil {
			return out, fmt.Errorf("published_at %q: %w", req.PublishedAt, err)
		}
		out.at = at.UTC()
	} else {
		out.at = time.Now().UTC()
	}
	for _, d := range req.Docs {
		fr, err := d.toFrozen(req.ModuleID)
		if err != nil {
			return out, err
		}
		treeSHA, err := svc.Freeze(ctx, fr)
		if err != nil {
			return out, err
		}
		out.docs = append(out.docs, frozenDocOut{fr: fr, treeSHA: treeSHA})
	}
	docs := make([]freeze.FrozenRevision, len(out.docs))
	for i := range out.docs {
		docs[i] = out.docs[i].fr
	}
	eventID, err := svc.Release(ctx, req.ModuleID, req.ReleaseID, docs, out.at)
	if err != nil {
		return out, err
	}
	out.eventID = eventID
	return out, nil
}

func printRelease(w io.Writer, line int, out releaseOut) {
	for _, d := range out.docs {
		fmt.Fprintf(w, "frozen line=%d doc_key=%s revision_id=%s tree_sha=%s content_sha256=%s\n",
			line, d.fr.DocKey, d.fr.RevisionID, d.treeSHA, d.fr.ContentSHA256)
	}
	fmt.Fprintf(w, "released line=%d module_id=%s release_id=%s published_at=%s docs=%d event_id=%s\n",
		line, out.req.ModuleID, out.req.ReleaseID, out.at.Format(time.RFC3339), len(out.docs), out.eventID)
}

// acceptDemo exercises the C-4 path end to end: it derives the first frozen
// document's tree, takes a real quote from its first paragraph (plus the
// section path leading there), and accepts a citation through the service,
// which reloads the canonical tree from the store.
func acceptDemo(ctx context.Context, svc *freeze.Service, doc frozenDocOut, w io.Writer) error {
	tree, err := structure.Derive(doc.fr.RevisionID, doc.fr.Source)
	if err != nil {
		return err
	}
	paras := tree.Paragraphs()
	if len(paras) == 0 {
		return fmt.Errorf("revision %s has no paragraphs to cite", doc.fr.RevisionID)
	}
	idx := paras[0]
	node := tree.Nodes[idx]
	text := strings.TrimSpace(string(doc.fr.Source[node.CharStart:node.CharEnd]))
	if text == "" {
		return fmt.Errorf("revision %s paragraph %d is empty", doc.fr.RevisionID, node.ParaIndex)
	}
	quote := firstNRunes(text, 60)
	path := sectionPathOf(tree, idx)

	req := citation.AcceptRequest{
		AnswerID: "freeze-worker-demo-answer",
		SearchID: "freeze-worker-demo-search",
		Citations: []citation.Citation{{
			DocKey:     doc.fr.DocKey,
			RevisionID: doc.fr.RevisionID,
			Locator: structure.Locator{
				SectionPath: path,
				ParaIndex:   node.ParaIndex,
				Quote:       quote,
			},
		}},
	}
	receipt, err := svc.Accept(ctx, req, doc.fr.RevisionID)
	if err != nil {
		return err
	}
	v := receipt.Verified[0]
	fmt.Fprintf(w, "locator para_index=%d section_path=%q quote=%q\n",
		node.ParaIndex, strings.Join(path, " / "), quote)
	fmt.Fprintf(w, "accepted answer_id=%s search_id=%s tree_sha=%s citations=%d node_id=%s span=[%d,%d)\n",
		receipt.AnswerID, receipt.SearchID, receipt.TreeSHA, len(receipt.Verified), v.Anchored.NodeID,
		v.Anchored.CharStart, v.Anchored.CharEnd)
	return nil
}

// sectionPathOf returns the heading chain enclosing node idx, matching the
// strictly-deepening semantics structure.Anchor resolves.
func sectionPathOf(t *structure.Tree, nodeIdx int) []string {
	var stack []structure.Node
	for i := 0; i < nodeIdx; i++ {
		n := t.Nodes[i]
		if n.Level == structure.LevelParagraph {
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].Level >= n.Level {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, n)
	}
	path := make([]string, len(stack))
	for i, n := range stack {
		path[i] = n.Title
	}
	return path
}

func firstNRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
