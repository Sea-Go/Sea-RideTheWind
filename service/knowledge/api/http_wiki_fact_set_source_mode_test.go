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
	asofRoot := os.Getenv("SEA_BTW_WIKI_ASOF_READER_ROOT")
	dwdRoot := os.Getenv("SEA_BTW_WIKI_DWD_SOURCE_ROOT")
	dwdRuntime := os.Getenv("SEA_BTW_WIKI_DWD_RUNTIME_ROOT")
	if btwRoot == "" {
		if readerRoot != "" || asofRoot != "" || dwdRoot != "" || dwdRuntime != "" {
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
	if asofRoot != "" {
		if readerRoot == "" || !filepath.IsAbs(asofRoot) ||
			filepath.Clean(asofRoot) != filepath.Clean(readerRoot) ||
			dwdRoot != "" || dwdRuntime != "" ||
			!filepath.IsAbs(os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR")) {
			return false, errors.New("Wiki as-of Holder needs the same FactSet/SourceProof tree and its own persistent evidence")
		}
	}
	if dwdRoot == "" && dwdRuntime != "" {
		return false, errors.New("Wiki DWD runtime cannot select a source Holder")
	}
	if dwdRoot != "" {
		if readerRoot == "" || !filepath.IsAbs(dwdRoot) ||
			filepath.Clean(dwdRoot) != filepath.Clean(readerRoot) ||
			!filepath.IsAbs(dwdRuntime) ||
			!filepath.IsAbs(os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR")) {
			return false, errors.New("Wiki DWD Holder requires the same SourceProof tree, fixed runtime and persistent evidence")
		}
		for _, path := range []string{
			filepath.Join(dwdRoot, "warehouse/wiki_quality/acceptance.py"),
			filepath.Join(dwdRoot, "internal/warehouse/wikiqualitydwd/export.go"),
		} {
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				return false, errors.New("Wiki DWD evidence loader is missing from the fixed source and runtime")
			}
		}
		// The locked venv Python is itself a symlink to a verified executable.
		for _, path := range []string{
			filepath.Join(dwdRuntime, "clickhouse"),
			filepath.Join(dwdRuntime, ".venv/bin/python"),
			filepath.Join(dwdRuntime, ".venv/bin/dbt"),
		} {
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
				return false, errors.New("Wiki DWD runtime is not an available executable")
			}
		}
	}
	if readerRoot != "" {
		script := filepath.Join(readerRoot,
			"internal/evaluation/wiki_quality/sourceproof/real-source-acceptance.sh")
		info, err := os.Lstat(script)
		if err != nil || !info.Mode().IsRegular() {
			return false, errors.New("FactSet SourceProof reader script is missing from fixed BTW tree")
		}
	}
	if os.Getenv("SEA_BTW_WIKI_ASOF_EVIDENCE_DIR") != "" {
		return false, errors.New("Wiki as-of evidence directory is Parent-owned and cannot select a Holder")
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
			t.Setenv("SEA_BTW_WIKI_ASOF_READER_ROOT", "")
			t.Setenv("SEA_BTW_WIKI_ASOF_EVIDENCE_DIR", "")
			t.Setenv("SEA_BTW_WIKI_DWD_SOURCE_ROOT", "")
			t.Setenv("SEA_BTW_WIKI_DWD_RUNTIME_ROOT", "")
			selected, err := wikiFactSetHolderMode()
			if selected != tc.selected || (err != nil) != tc.invalid {
				t.Fatalf("early Holder selector misrouted shared DC/quality/qrel: selected=%v err=%v", selected, err)
			}
		})
	}
}

func TestWikiFactSetHolderSourceProofReaderNeedsFixedScriptBeforePG(t *testing.T) {
	t.Setenv("SEA_BTW_WIKI_DWD_SOURCE_ROOT", "")
	t.Setenv("SEA_BTW_WIKI_DWD_RUNTIME_ROOT", "")
	t.Setenv("SEA_BTW_WIKI_ASOF_READER_ROOT", "")
	t.Setenv("SEA_BTW_WIKI_ASOF_EVIDENCE_DIR", "")
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

func TestWikiFactSetDWDHolderNeedsOneFixedReaderRuntimeAndPersistentEvidence(t *testing.T) {
	t.Setenv("SEA_BTW_WIKI_ASOF_READER_ROOT", "")
	t.Setenv("SEA_BTW_WIKI_ASOF_EVIDENCE_DIR", "")
	root, runtime := t.TempDir(), t.TempDir()
	for _, path := range []string{
		filepath.Join(root, "internal/evaluation/wiki_quality/sourceproof/real-source-acceptance.sh"),
		filepath.Join(root, "warehouse/wiki_quality/acceptance.py"),
		filepath.Join(root, "internal/warehouse/wikiqualitydwd/export.go"),
		filepath.Join(runtime, "clickhouse"),
		filepath.Join(runtime, ".venv/bin/python"),
		filepath.Join(runtime, ".venv/bin/dbt"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test-only source or runtime fixture"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SEA_BTW_WIKI_FACT_SET_CONSUMER_ROOT", root)
	t.Setenv("SEA_BTW_SOURCEPROOF_READER_ROOT", root)
	t.Setenv("SEA_DC_EVENT_PLATFORM_ROOT", t.TempDir())
	t.Setenv("SEA_BTW_WIKI_QUALITY_CONSUMER_ROOT", "")
	t.Setenv("SEA_BTW_SEARCHSOURCE_CONSUMER_ROOT", "")
	t.Setenv("KNOWLEDGE_FACT_SET_REAL_HTTP", "")
	t.Setenv("SEA_BTW_WIKI_DWD_SOURCE_ROOT", t.TempDir())
	t.Setenv("SEA_BTW_WIKI_DWD_RUNTIME_ROOT", runtime)
	t.Setenv("KNOWLEDGE_OBS_EVIDENCE_DIR", t.TempDir())
	if selected, err := wikiFactSetHolderMode(); selected || err == nil {
		t.Fatalf("DWD Holder accepted a different BTW producer tree: %v %v", selected, err)
	}
	t.Setenv("SEA_BTW_WIKI_DWD_SOURCE_ROOT", root)
	t.Setenv("KNOWLEDGE_OBS_EVIDENCE_DIR", "")
	if selected, err := wikiFactSetHolderMode(); selected || err == nil {
		t.Fatalf("DWD Holder accepted ephemeral original evidence: %v %v", selected, err)
	}
	t.Setenv("KNOWLEDGE_OBS_EVIDENCE_DIR", t.TempDir())
	if selected, err := wikiFactSetHolderMode(); !selected || err != nil {
		t.Fatalf("complete DWD selector rejected before disposable PG: %v %v", selected, err)
	}
}

func TestWikiFactSetAsOfHolderNeedsOneSourceProofTreeAndParentEvidence(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	script := filepath.Join(root,
		"internal/evaluation/wiki_quality/sourceproof/real-source-acceptance.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEA_BTW_WIKI_FACT_SET_CONSUMER_ROOT", root)
	t.Setenv("SEA_BTW_SOURCEPROOF_READER_ROOT", root)
	t.Setenv("SEA_DC_EVENT_PLATFORM_ROOT", t.TempDir())
	t.Setenv("KNOWLEDGE_OBS_EVIDENCE_DIR", t.TempDir())
	t.Setenv("SEA_BTW_WIKI_DWD_SOURCE_ROOT", "")
	t.Setenv("SEA_BTW_WIKI_DWD_RUNTIME_ROOT", "")
	t.Setenv("SEA_BTW_WIKI_QUALITY_CONSUMER_ROOT", "")
	t.Setenv("SEA_BTW_SEARCHSOURCE_CONSUMER_ROOT", "")
	t.Setenv("KNOWLEDGE_FACT_SET_REAL_HTTP", "")
	t.Setenv("SEA_BTW_WIKI_ASOF_EVIDENCE_DIR", "")
	for _, tc := range []struct {
		name, root, reader, evidence, dwd string
		selected                          bool
	}{
		{"legacy SourceProof unchanged", "", root, "", "", true},
		{"as-of cannot select alone", root, "", "", "", false},
		{"as-of different BTW leaf rejected", other, root, "", "", false},
		{"as-of relative root rejected", "relative-asof", root, "", "", false},
		{"as-of cannot share DWD mode", root, root, "", root, false},
		{"external as-of evidence rejected", root, root, other, "", false},
		{"same fixed root selected", root, root, "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SEA_BTW_WIKI_ASOF_READER_ROOT", tc.root)
			t.Setenv("SEA_BTW_SOURCEPROOF_READER_ROOT", tc.reader)
			t.Setenv("SEA_BTW_WIKI_ASOF_EVIDENCE_DIR", tc.evidence)
			t.Setenv("SEA_BTW_WIKI_DWD_SOURCE_ROOT", tc.dwd)
			selected, err := wikiFactSetHolderMode()
			if selected != tc.selected || (err == nil) != tc.selected {
				t.Fatalf("as-of selector crossed old product/cursor mode: selected=%v err=%v", selected, err)
			}
		})
	}
	t.Setenv("SEA_BTW_WIKI_ASOF_READER_ROOT", root)
	t.Setenv("KNOWLEDGE_OBS_EVIDENCE_DIR", "")
	if selected, err := wikiFactSetHolderMode(); selected || err == nil {
		t.Fatalf("as-of without Parent evidence crossed PG boundary: %v %v", selected, err)
	}
}
