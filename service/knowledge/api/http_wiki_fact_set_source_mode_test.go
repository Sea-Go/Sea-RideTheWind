package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// An independent DC event platform root is not itself a Holder selector.
// Validate the one selected producer/consumer script before any PG effect.
func wikiFactSetHolderMode() (bool, error) {
	btwRoot := os.Getenv("SEA_BTW_WIKI_FACT_SET_CONSUMER_ROOT")
	readerRoot := os.Getenv("SEA_BTW_SOURCEPROOF_READER_ROOT")
	if btwRoot == "" {
		if readerRoot != "" {
			return false, errors.New("FactSet SourceProof reader requires the original FactSet Holder")
		}
		return false, nil
	}
	dcRoot := os.Getenv("SEA_DC_EVENT_PLATFORM_ROOT")
	if dcRoot == "" || !filepath.IsAbs(btwRoot) || !filepath.IsAbs(dcRoot) {
		return false, errors.New("FactSet Holder needs both ordinary absolute BTW and shared DC roots")
	}
	if readerRoot != "" && (!filepath.IsAbs(readerRoot) ||
		filepath.Clean(readerRoot) != filepath.Clean(btwRoot)) {
		return false, errors.New("FactSet SourceProof reader must use the same fixed BTW source tree")
	}
	if readerRoot != "" {
		script := filepath.Join(readerRoot,
			"internal/evaluation/wiki_quality/sourceproof/real-source-acceptance.sh")
		info, err := os.Lstat(script)
		if err != nil || !info.Mode().IsRegular() {
			return false, errors.New("FactSet SourceProof reader script is missing from fixed BTW tree")
		}
	}
	if os.Getenv("SEA_BTW_WIKI_QUALITY_CONSUMER_ROOT") != "" ||
		os.Getenv("SEA_BTW_SEARCHSOURCE_CONSUMER_ROOT") != "" ||
		os.Getenv("KNOWLEDGE_FACT_SET_REAL_HTTP") == "1" {
		return false, errors.New("FactSet source Holder cannot share a cursor with another source fixture")
	}
	return true, nil
}

func TestWikiFactSetHolderSelectorRejectsIncompleteOrSharedModes(t *testing.T) {
	for _, tc := range []struct {
		name, btw, dc, quality, search, single, reader string
		selected                                       bool
		invalid                                        bool
	}{
		{"default old workflow", "", "", "", "", "", "", false, false},
		{"shared DC alone does not select", "", "/tmp/dc-root", "", "", "", "", false, false},
		{"own BTW root with missing DC", "/tmp/btw-root", "", "", "", "", "", false, true},
		{"relative BTW root forbidden", "relative-btw", "/tmp/dc-root", "", "", "", "", false, true},
		{"exclusive two-root Holder", "/tmp/btw-root", "/tmp/dc-root", "", "", "", "", true, false},
		{"quality Holder cannot coexist", "/tmp/btw-root", "/tmp/dc-root", "/tmp/quality", "", "", "", false, true},
		{"qrel Holder cannot coexist", "/tmp/btw-root", "/tmp/dc-root", "", "/tmp/search", "", "", false, true},
		{"single-http fixture cannot coexist", "/tmp/btw-root", "/tmp/dc-root", "", "", "1", "", false, true},
		{"Reader without Holder rejected", "", "/tmp/dc-root", "", "", "", "/tmp/btw-root", false, true},
		{"Reader different BTW tree is invalid", "/tmp/btw-root", "/tmp/dc-root", "", "", "", "/tmp/other-root", false, true},
		{"Reader relative root is invalid", "/tmp/btw-root", "/tmp/dc-root", "", "", "", "relative-reader", false, true},
		{"Reader same root without script rejected before PG", "/tmp/btw-root", "/tmp/dc-root", "", "", "", "/tmp/btw-root", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SEA_BTW_WIKI_FACT_SET_CONSUMER_ROOT", tc.btw)
			t.Setenv("SEA_DC_EVENT_PLATFORM_ROOT", tc.dc)
			t.Setenv("SEA_BTW_WIKI_QUALITY_CONSUMER_ROOT", tc.quality)
			t.Setenv("SEA_BTW_SEARCHSOURCE_CONSUMER_ROOT", tc.search)
			t.Setenv("KNOWLEDGE_FACT_SET_REAL_HTTP", tc.single)
			t.Setenv("SEA_BTW_SOURCEPROOF_READER_ROOT", tc.reader)
			selected, err := wikiFactSetHolderMode()
			if selected != tc.selected || (err != nil) != tc.invalid {
				t.Fatalf("early Holder selector misrouted shared DC/quality/qrel: selected=%v err=%v", selected, err)
			}
		})
	}
}

func TestWikiFactSetHolderSourceProofReaderNeedsFixedScriptBeforePG(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root,
		"internal/evaluation/wiki_quality/sourceproof/real-source-acceptance.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEA_BTW_WIKI_FACT_SET_CONSUMER_ROOT", root)
	t.Setenv("SEA_BTW_SOURCEPROOF_READER_ROOT", root)
	t.Setenv("SEA_DC_EVENT_PLATFORM_ROOT", t.TempDir())
	t.Setenv("SEA_BTW_WIKI_QUALITY_CONSUMER_ROOT", "")
	t.Setenv("SEA_BTW_SEARCHSOURCE_CONSUMER_ROOT", "")
	t.Setenv("KNOWLEDGE_FACT_SET_REAL_HTTP", "")
	selected, err := wikiFactSetHolderMode()
	if selected || err == nil {
		t.Fatalf("missing BTW Reader script reached disposable PG: %v %v", selected, err)
	}
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\n"), 0600); err != nil {
		t.Fatal(err)
	}
	selected, err = wikiFactSetHolderMode()
	if !selected || err != nil {
		t.Fatalf("fixed ordinary BTW Reader script was rejected: %v %v", selected, err)
	}
}
