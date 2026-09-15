package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

type realWikiDWDEvidence struct {
	ParentVerified bool
	ManifestSHA    string
	CHReportSHA    string
	CHStopSHA      string
	ADSFacts       int
	QualityState   string
}

type realWikiDWDManifest struct {
	Schema               string `json:"schema"`
	Producer             string `json:"producer"`
	Consumer             string `json:"consumer"`
	ModuleID             string `json:"module_id"`
	PageID               string `json:"page_id"`
	WikiRevisionID       string `json:"wiki_revision_id"`
	FactSetRevisionID    string `json:"fact_set_revision_id"`
	SourceScopeRevision  string `json:"source_scope_revision"`
	CutoffOffset         int64  `json:"cutoff_offset"`
	AcknowledgedAtLeast  int64  `json:"acknowledged_at_least"`
	CommittedAtLeast     int64  `json:"committed_at_least"`
	ODSEvidenceSHA256    string `json:"ods_evidence_sha256"`
	DCIndexSHA256        string `json:"dc_index_sha256"`
	PrefixJSONLSHA256    string `json:"prefix_jsonl_sha256"`
	CatalogJSONLSHA256   string `json:"catalog_jsonl_sha256"`
	JudgmentJSONLSHA256  string `json:"judgment_jsonl_sha256"`
	PrefixRows           int    `json:"prefix_rows"`
	CatalogFacts         int    `json:"catalog_facts"`
	JudgmentRevisions    int    `json:"judgment_revisions"`
	TechnicalSkips       int    `json:"technical_skips"`
	TransportedFactCount int    `json:"transported_fact_count"`
	EvidenceLevel        string `json:"evidence_level"`
	QualityState         string `json:"quality_state"`
	Activation           string `json:"activation"`
}

// This directory is outside the RTW test's t.TempDir: all four original Go
// source files remain available after the source processes and PG have stopped.
func preparePersistentWikiDWDBundle(t *testing.T, evidenceDir string) string {
	t.Helper()
	if !filepath.IsAbs(evidenceDir) || evidenceDir == "" {
		t.Fatal("Wiki DWD needs a persistent absolute acceptance evidence root")
	}
	if err := os.MkdirAll(evidenceDir, 0700); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Lstat(evidenceDir)
	if err != nil || !parent.IsDir() || parent.Mode().Perm() != 0700 {
		t.Fatalf("Wiki DWD parent is not an ordinary private evidence directory: %v %v", parent, err)
	}
	path := filepath.Join(evidenceDir, "wiki-factset-dwd-bundle")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatalf("Wiki DWD bundle generation must be new: %v", err)
	}
	info, err := os.Lstat(path)
	entries, readErr := os.ReadDir(path)
	if err != nil || readErr != nil || !info.IsDir() ||
		info.Mode().Perm() != 0700 || len(entries) != 0 {
		t.Fatalf("Wiki DWD source bundle is not a fresh ordinary 0700 directory: %v %v", err, readErr)
	}
	return path
}

func readPersistentWikiDWDFile(t *testing.T, path string, maxBytes int64) []byte {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 ||
		info.Size() < 1 || info.Size() > maxBytes {
		t.Fatalf("Wiki DWD original file is absent, replaced or not exact 0600: %s %v", path, err)
	}
	body, err := os.ReadFile(path)
	if err != nil || int64(len(body)) != info.Size() {
		t.Fatalf("Wiki DWD original file changed while reading: %s %v", path, err)
	}
	return body
}

func verifyRealWikiDWDBundle(t *testing.T, directory string,
	catalog types.WikiFactSetRecord, sourceProof realWikiFactSetSourceProofResult,
	dcIndex string, events, skips int) (realWikiDWDManifest, string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 4 {
		t.Fatalf("Wiki DWD bundle must contain exactly four original files: %v %v", entries, err)
	}
	manifestRaw := readPersistentWikiDWDFile(t, filepath.Join(directory, "manifest.json"), 32<<10)
	canonical, err := jsoncanonicalizer.Transform(manifestRaw)
	if err != nil || !bytes.Equal(canonical, manifestRaw) {
		t.Fatalf("Wiki DWD source manifest is not its original JCS bytes: %v", err)
	}
	var manifest realWikiDWDManifest
	decoder := json.NewDecoder(bytes.NewReader(manifestRaw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("Wiki DWD source manifest has an unknown or malformed field: %v", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		t.Fatalf("Wiki DWD source manifest has a trailing value: %v", err)
	}
	if manifest.Schema != "sea.wiki.fact-set-offline-source.v1" ||
		manifest.Producer != "ridethewind.knowledge" ||
		manifest.Consumer != "btw-warehouse-wiki-quality" ||
		manifest.ModuleID != catalog.ModuleId || manifest.PageID != catalog.PageId ||
		manifest.WikiRevisionID != catalog.WikiRevisionId ||
		manifest.FactSetRevisionID != catalog.FactSetRevisionId ||
		manifest.SourceScopeRevision != catalog.SourceScopeRevision ||
		manifest.CutoffOffset != int64(events) || manifest.PrefixRows != events ||
		manifest.CatalogFacts != 2 || manifest.JudgmentRevisions != 2 ||
		manifest.TechnicalSkips != skips || manifest.TransportedFactCount != 2 ||
		manifest.CommittedAtLeast < int64(events) ||
		manifest.AcknowledgedAtLeast < int64(events) ||
		manifest.ODSEvidenceSHA256 != sourceProof.ODSEvidenceSHA256 ||
		manifest.DCIndexSHA256 != sourceProof.DCIndexSHA256 ||
		manifest.DCIndexSHA256 != dcIndex ||
		manifest.EvidenceLevel != "rtw_dc_source_proof_only" ||
		manifest.QualityState != "not_evaluable" || manifest.Activation != "none" {
		t.Fatalf("Wiki DWD source bundle promoted a claim or lost the same ACK cutoff: %+v", manifest)
	}
	for _, artifact := range []struct {
		name, digest string
		rows         int
	}{{"prefix.jsonl", manifest.PrefixJSONLSHA256, events},
		{"catalog-facts.jsonl", manifest.CatalogJSONLSHA256, 2},
		{"judgments.jsonl", manifest.JudgmentJSONLSHA256, 2}} {
		body := readPersistentWikiDWDFile(t, filepath.Join(directory, artifact.name), 16<<20)
		if !bytes.HasSuffix(body, []byte("\n")) ||
			bytes.Count(body, []byte("\n")) != artifact.rows ||
			object.Hash(body) != artifact.digest {
			t.Fatalf("Wiki DWD original %s bytes/rows differ from caller-pinned manifest", artifact.name)
		}
	}
	return manifest, object.Hash(manifestRaw)
}

type realWikiDWDCHReport struct {
	Status                               string `json:"status"`
	EvidenceLevel                        string `json:"evidence_level"`
	QualityState                         string `json:"quality_state"`
	Activation                           string `json:"activation"`
	SameGenerationReinsertRejected       bool   `json:"same_generation_reinsert_rejected"`
	SourceAuthorityIndependentlyVerified bool   `json:"source_authority_independently_verified_here"`
	D07Evaluable                         bool   `json:"d07_evaluable"`
	ProductionVerified                   bool   `json:"production_verified"`
	Cutoffs                              map[string]struct {
		PrefixRows           int               `json:"prefix_rows"`
		JudgmentRevisions    int               `json:"judgment_revisions"`
		TechnicalSkips       int               `json:"technical_skips"`
		ADSFacts             int               `json:"ads_rows"`
		SourceManifestSHA256 string            `json:"source_manifest_sha256"`
		LastTransported      map[string]string `json:"last_transported_revisions"`
	} `json:"cutoffs"`
}

func verifyAndBuildRealWikiDWD(t *testing.T, ctx context.Context,
	btwRoot, runtimeRoot, evidenceDir, bundleDir string,
	catalog types.WikiFactSetRecord, sourceProof realWikiFactSetSourceProofResult,
	dcIndex string, events, skips int) realWikiDWDEvidence {
	t.Helper()
	manifest, manifestSHA := verifyRealWikiDWDBundle(t, bundleDir,
		catalog, sourceProof, dcIndex, events, skips)
	outputDir := filepath.Join(evidenceDir, "wiki-factset-dwd-ch")
	command := exec.CommandContext(ctx, filepath.Join(runtimeRoot, ".venv/bin/python"),
		filepath.Join(btwRoot, "warehouse/wiki_quality/acceptance.py"),
		"--runtime", runtimeRoot, "--output", outputDir,
		"--bundle", bundleDir, "--expected-manifest-sha256", manifestSHA)
	command.Dir = btwRoot
	output, runErr := command.CombinedOutput()
	logPath := filepath.Join(evidenceDir, "wiki-factset-dwd-ch-run.log")
	writeRealFactSet0600(t, logPath, output)
	if runErr != nil {
		t.Fatalf("caller-pinned Wiki DWD CH/dbt bundle rejected: %v; task-log=%s", runErr, logPath)
	}
	reportRaw, err := os.ReadFile(filepath.Join(outputDir, "report.json"))
	if err != nil {
		t.Fatalf("Wiki DWD CH report missing after successful loader: %v", err)
	}
	var report realWikiDWDCHReport
	if err := json.Unmarshal(reportRaw, &report); err != nil {
		t.Fatalf("Wiki DWD CH report malformed: %v", err)
	}
	cutoff := report.Cutoffs[strconv.Itoa(events)]
	if report.Status != "passed" || report.QualityState != "not_evaluable" ||
		report.EvidenceLevel != "caller_pinned_Go_bundle_real_CH_dbt_source_authority_separate" ||
		report.Activation != "none" || report.D07Evaluable || report.ProductionVerified ||
		report.SourceAuthorityIndependentlyVerified ||
		!report.SameGenerationReinsertRejected || len(report.Cutoffs) != 1 ||
		cutoff.PrefixRows != manifest.PrefixRows ||
		cutoff.JudgmentRevisions != manifest.JudgmentRevisions ||
		cutoff.TechnicalSkips != manifest.TechnicalSkips ||
		cutoff.ADSFacts != manifest.CatalogFacts ||
		cutoff.SourceManifestSHA256 != manifestSHA ||
		len(cutoff.LastTransported) != 2 {
		t.Fatalf("Wiki DWD CH/dbt report replaced the acknowledged source with a human label: %+v", report)
	}
	for _, fact := range catalog.Facts {
		if !fact.Required || cutoff.LastTransported[fact.FactId] != "1" {
			t.Fatalf("Wiki DWD CH last transported revision differs from target required Fact: %s", fact.FactId)
		}
	}
	stopRaw, err := os.ReadFile(filepath.Join(outputDir, "ch-stop-status.json"))
	if err != nil {
		t.Fatalf("Wiki DWD CH process stop status missing: %v", err)
	}
	var stop struct {
		ProcessExited       bool `json:"process_exited"`
		EndpointUnreachable bool `json:"endpoint_unreachable"`
		ExitCode            int  `json:"exit_code"`
	}
	if json.Unmarshal(stopRaw, &stop) != nil || !stop.ProcessExited ||
		!stop.EndpointUnreachable || stop.ExitCode != 0 {
		t.Fatalf("Wiki DWD CH task process is still live: %+v", stop)
	}
	runResultsPath := filepath.Join(outputDir,
		fmt.Sprintf("build-c%d/target/run_results.json", events))
	runInfo, err := os.Lstat(runResultsPath)
	if err != nil || !runInfo.Mode().IsRegular() ||
		runInfo.Size() < 1 || runInfo.Size() > 2<<20 {
		t.Fatalf("Wiki DWD dbt results absent or replaced: %v", err)
	}
	runResultsRaw, err := os.ReadFile(runResultsPath)
	if err != nil || int64(len(runResultsRaw)) != runInfo.Size() {
		t.Fatalf("Wiki DWD dbt results changed while reading: %v", err)
	}
	var runResults struct {
		Results []struct {
			Status string `json:"status"`
		} `json:"results"`
	}
	if json.Unmarshal(runResultsRaw, &runResults) != nil || len(runResults.Results) != 8 {
		t.Fatal("Wiki DWD CH dbt four models and four tests were not all built")
	}
	var built, tested int
	for _, row := range runResults.Results {
		switch row.Status {
		case "success":
			built++
		case "pass":
			tested++
		default:
			t.Fatalf("Wiki DWD CH dbt node failed: %s", row.Status)
		}
	}
	if built != 4 || tested != 4 {
		t.Fatalf("Wiki DWD CH dbt sources/models/tests incomplete: built=%d tested=%d", built, tested)
	}
	return realWikiDWDEvidence{ParentVerified: true, ManifestSHA: manifestSHA,
		CHReportSHA: object.Hash(reportRaw), CHStopSHA: object.Hash(stopRaw),
		ADSFacts: cutoff.ADSFacts, QualityState: report.QualityState}
}
