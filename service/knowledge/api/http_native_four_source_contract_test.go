package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeFourSourceSelectorRequiresOneFixedBuilderAndPrivateBGE(t *testing.T) {
	for _, key := range []string{"SEA_RTW_NATIVE_FOUR_SOURCE", "SEA_BTW_PRODUCT_SEARCH_ROOT",
		"SEA_BTW_SEARCH_API_SOCKET_ROOT", "SEA_BTW_NATIVE_SOURCE_SHA",
		"SEA_DC_BGE_RUNTIME", "SEA_RTW_NATIVE_PYTHON",
		"KNOWLEDGE_OBS_EVIDENCE_DIR",
		"SEA_BGE_WORKER_PUBLISH_SEARCH", "SEA_BGE_WORKER_CANCEL_RELEASE",
		"SEA_BGE_WORKER_PROCESSES", "SEA_BTW_WIKI_FACT_SET_CONSUMER_ROOT",
		"SEA_DC_EVENT_PLATFORM_ROOT", "SEA_BTW_SUMMARY_DC_RUNTIME_FILE",
		"SEA_BTW_NATIVE_PUBLISHED_WITNESS_FIXTURE"} {
		t.Setenv(key, "")
	}
	if enabled, err := nativeFourSourceMode(false); err != nil || enabled {
		t.Fatalf("default RTW workflow unexpectedly selected Native four Source: %t %v", enabled, err)
	}
	btw := t.TempDir()
	for _, args := range [][]string{{"-C", btw, "init", "-q"},
		{"-C", btw, "config", "user.name", "test-owner"},
		{"-C", btw, "config", "user.email", "test-owner@example.invalid"}} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("temp fixed BTW Git source setup failed: %v %s", err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(btw, "fixed.txt"), []byte("one fixed Native source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-C", btw, "add", "fixed.txt"},
		{"-C", btw, "commit", "-q", "-m", "fixed test source"}} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("temp BTW source commit failed: %v %s", err, output)
		}
	}
	shaRaw, err := exec.Command("git", "-C", btw, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	fixedSHA := strings.TrimSpace(string(shaRaw))
	python := filepath.Join(t.TempDir(), "test-python")
	if err := os.WriteFile(python, []byte("#!/bin/sh\nprintf '3.2.1 1.15.0\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	dcRuntime := filepath.Join(t.TempDir(), "runtime.json")
	if err := os.WriteFile(dcRuntime, []byte(`{"fixed":"bge"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEA_RTW_NATIVE_FOUR_SOURCE", "1")
	t.Setenv("SEA_BTW_PRODUCT_SEARCH_ROOT", btw)
	t.Setenv("SEA_BTW_SEARCH_API_SOCKET_ROOT", btw)
	t.Setenv("SEA_BTW_NATIVE_SOURCE_SHA", fixedSHA)
	t.Setenv("SEA_DC_BGE_RUNTIME", dcRuntime)
	t.Setenv("SEA_RTW_NATIVE_PYTHON", python)
	t.Setenv("KNOWLEDGE_OBS_EVIDENCE_DIR", t.TempDir())
	if enabled, err := nativeFourSourceMode(false); err != nil || !enabled {
		t.Fatalf("one fixed BTW tree/private BGE should allow test selector: %t %v", enabled, err)
	}
	t.Setenv("SEA_BTW_NATIVE_SOURCE_SHA", strings.Repeat("0", 40))
	if enabled, err := nativeFourSourceMode(false); err == nil || enabled {
		t.Fatal("Native source accepted a changed BTW HEAD")
	}
	t.Setenv("SEA_BTW_NATIVE_SOURCE_SHA", fixedSHA)
	if enabled, err := nativeFourSourceMode(true); err == nil || enabled {
		t.Fatal("Native four Source claimed a real UserCenter acceptance it has not run")
	}
	t.Setenv("SEA_BTW_SEARCH_API_SOCKET_ROOT", t.TempDir())
	if enabled, err := nativeFourSourceMode(false); err == nil || enabled {
		t.Fatal("two different BTW builder/formal trees were accepted")
	}
	t.Setenv("SEA_BTW_SEARCH_API_SOCKET_ROOT", btw)
	if err := os.Chmod(dcRuntime, 0644); err != nil {
		t.Fatal(err)
	}
	if enabled, err := nativeFourSourceMode(false); err == nil || enabled {
		t.Fatal("public BGE runtime reached Native test PG/model")
	}
	if err := os.Chmod(dcRuntime, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEA_BTW_SUMMARY_DC_RUNTIME_FILE", dcRuntime)
	if enabled, err := nativeFourSourceMode(false); err == nil || enabled {
		t.Fatal("Native source discovery was mixed with a separate summary model mode")
	}
	t.Setenv("SEA_BTW_SUMMARY_DC_RUNTIME_FILE", "")
	if err := os.WriteFile(python, []byte("#!/bin/sh\nprintf '2.5.1 1.15.0\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if enabled, err := nativeFourSourceMode(false); err == nil || enabled {
		t.Fatal("unpinned Lite version reached Native PG/model")
	}
}

func TestNativeLiteRuntimeRequiresTheV2PackageOwner(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "runtime.json")
	base := realNativeLiteRuntime{SchemaVersion: "sea.search.native-lite-runtime.v2",
		Owner: realNativeLiteOwner, Endpoint: "127.0.0.1:19530", Engine: "lite",
		MilvusLite: "3.2.1", EnginePackageSHA256: realNativeLitePackageSHA,
		FAISSVersion: "1.15.0", Directory: dir,
		ReleasePath: filepath.Join(dir, "release")}
	write := func(value realNativeLiteRuntime, perm os.FileMode) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, perm); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, perm); err != nil {
			t.Fatal(err)
		}
	}
	write(base, 0600)
	if runtime, err := readRealNativeLiteRuntime(path); err != nil ||
		runtime.SchemaVersion != base.SchemaVersion || runtime.RawSHA256 == "" ||
		runtime.EnginePackageSHA256 != realNativeLitePackageSHA {
		t.Fatalf("owned native v2 runtime was rejected: %+v %v", runtime, err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*realNativeLiteRuntime)
		perm   os.FileMode
	}{
		{"old binary runtime", func(r *realNativeLiteRuntime) { r.SchemaVersion = "sea.search.native-lite-runtime.v1" }, 0600},
		{"wrong package bytes", func(r *realNativeLiteRuntime) {
			r.EnginePackageSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		}, 0600},
		{"wrong FAISS", func(r *realNativeLiteRuntime) { r.FAISSVersion = "2.5.1" }, 0600},
		{"unowned process", func(r *realNativeLiteRuntime) { r.Owner = "old Sparse-only Lite" }, 0600},
		{"public runtime", func(*realNativeLiteRuntime) {}, 0644},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := base
			test.mutate(&candidate)
			write(candidate, test.perm)
			if _, err := readRealNativeLiteRuntime(path); err == nil {
				t.Fatal("wrong Native physical package or owner was accepted")
			}
		})
	}
}

func TestNativeSettingsUseTheFormalLowercaseLiteContract(t *testing.T) {
	valid := []byte(`{"schema_version":"sea.search.native-backends.v2","engine":"lite",` +
		`"namespace":"native_123456abcdef","sparse_backend":"frozen_ip_postings",` +
		`"dense":{"m":16,"ef_construction":128,"ef_search":64},` +
		`"multivector":{"m":16,"ef_construction":128,"ef_search":64}}`)
	if !validRealNativeSettings(valid) {
		t.Fatal("BTW owner native settings did not match formal API config")
	}
	for _, invalid := range [][]byte{
		[]byte(`{"SchemaVersion":"sea.search.native-backends.v2"}`),
		append(append([]byte(nil), valid[:len(valid)-1]...), []byte(`,"tenant_id":"platform"}`)...),
		[]byte(strings.Replace(string(valid), `"sparse_backend":"frozen_ip_postings"`,
			`"sparse_backend":"milvus_bm25"`, 1)),
		[]byte(strings.Replace(string(valid), `"sparse_backend":"frozen_ip_postings"`,
			`"sparse_backend":"milvus_ip"`, 1)),
		[]byte(strings.Replace(string(valid), `"ef_search":64}`, `"ef_search":2}`, 1)),
	} {
		if validRealNativeSettings(invalid) {
			t.Fatalf("nonformal Native settings were accepted: %s", invalid)
		}
	}
}
