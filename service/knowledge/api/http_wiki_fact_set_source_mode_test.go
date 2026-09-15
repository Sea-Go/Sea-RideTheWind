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
	if btwRoot == "" {
		return false, nil
	}
	dcRoot := os.Getenv("SEA_DC_EVENT_PLATFORM_ROOT")
	if dcRoot == "" || !filepath.IsAbs(btwRoot) || !filepath.IsAbs(dcRoot) {
		return false, errors.New("FactSet Holder needs both ordinary absolute BTW and shared DC roots")
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
		name, btw, dc, quality, search, single string
		selected                               bool
		invalid                                bool
	}{
		{"default old workflow", "", "", "", "", "", false, false},
		{"shared DC alone does not select", "", "/tmp/dc-root", "", "", "", false, false},
		{"own BTW root with missing DC", "/tmp/btw-root", "", "", "", "", false, true},
		{"relative BTW root forbidden", "relative-btw", "/tmp/dc-root", "", "", "", false, true},
		{"exclusive two-root Holder", "/tmp/btw-root", "/tmp/dc-root", "", "", "", true, false},
		{"quality Holder cannot coexist", "/tmp/btw-root", "/tmp/dc-root", "/tmp/quality", "", "", false, true},
		{"qrel Holder cannot coexist", "/tmp/btw-root", "/tmp/dc-root", "", "/tmp/search", "", false, true},
		{"single-http fixture cannot coexist", "/tmp/btw-root", "/tmp/dc-root", "", "", "1", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SEA_BTW_WIKI_FACT_SET_CONSUMER_ROOT", tc.btw)
			t.Setenv("SEA_DC_EVENT_PLATFORM_ROOT", tc.dc)
			t.Setenv("SEA_BTW_WIKI_QUALITY_CONSUMER_ROOT", tc.quality)
			t.Setenv("SEA_BTW_SEARCHSOURCE_CONSUMER_ROOT", tc.search)
			t.Setenv("KNOWLEDGE_FACT_SET_REAL_HTTP", tc.single)
			selected, err := wikiFactSetHolderMode()
			if selected != tc.selected || (err != nil) != tc.invalid {
				t.Fatalf("early Holder selector misrouted shared DC/quality/qrel: selected=%v err=%v", selected, err)
			}
		})
	}
}
