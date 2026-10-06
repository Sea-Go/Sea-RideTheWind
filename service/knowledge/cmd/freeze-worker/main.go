// freeze-worker is the dev entry of the M1 wiring skeleton (C-14): it reads
// a JSONL file of freeze/release requests, runs Freeze+Release over
// in-memory stores, and prints the deterministic event id of each release.
// With --accept it additionally demonstrates the C-4 citation acceptance
// path using a locator quote taken from a real paragraph of the first
// frozen document.
//
// Usage:
//
//	go run ./service/knowledge/cmd/freeze-worker \
//	  --input service/knowledge/cmd/freeze-worker/testdata/sample-freeze.jsonl \
//	  --accept
//
// Input format (one release request per line, blank lines skipped):
//
//	{"module_id":"m","release_id":"r","published_at":"2026-10-06T12:00:00Z",
//	 "docs":[{"revision_id":"rev-1","doc_key":"source:<uuid>@v1",
//	          "content_sha256":"<64hex|omit to compute>","source_b64":"..."}]}
//
// published_at is RFC3339; omitting it stamps the current UTC time (and
// therefore changes the event id run to run).
package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseFlags(args, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "usage: freeze-worker --input <jsonl|-> [--accept]")
		return 2
	}
	reader, closeFn, err := openInput(opts.input)
	if err != nil {
		fmt.Fprintf(stderr, "open %s: %v\n", opts.input, err)
		return 1
	}
	defer closeFn()

	requests, err := readRequests(reader)
	if err != nil {
		fmt.Fprintf(stderr, "read requests: %v\n", err)
		return 1
	}
	if len(requests) == 0 {
		fmt.Fprintln(stderr, "no release requests in input")
		return 1
	}

	svc := newService()
	ctx := context.Background()
	var demoDoc *frozenDocOut
	for i, req := range requests {
		out, err := processRequest(ctx, svc, req)
		if err != nil {
			fmt.Fprintf(stderr, "line %d: %v\n", i+1, err)
			return 1
		}
		printRelease(stdout, i+1, out)
		if demoDoc == nil && len(out.docs) > 0 {
			demoDoc = &out.docs[0]
		}
	}
	if opts.accept && demoDoc != nil {
		if err := acceptDemo(ctx, svc, *demoDoc, stdout); err != nil {
			fmt.Fprintf(stderr, "accept demo: %v\n", err)
			return 1
		}
	}
	return 0
}
