package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"sea-try-go/service/knowledge/api/internal/object"
)

const realNativeLiteOwner = "Search native three-lane isolated acceptance only"
const realNativeLitePackageSHA = "d713528aff96e15310ec764e153d4e046da93644820b7eafdfbb2623a571524c"

type realNativeLiteRuntime struct {
	SchemaVersion       string `json:"schema_version"`
	Owner               string `json:"owner"`
	Endpoint            string `json:"endpoint"`
	Engine              string `json:"engine"`
	MilvusLite          string `json:"milvus_lite"`
	EnginePackageSHA256 string `json:"engine_package_sha256"`
	FAISSVersion        string `json:"faiss_version"`
	Directory           string `json:"directory"`
	ReleasePath         string `json:"release_path"`
	RawSHA256           string `json:"-"`
}

func realNativeHash(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func readRealNativeLiteRuntime(path string) (realNativeLiteRuntime, error) {
	var runtime realNativeLiteRuntime
	if !filepath.IsAbs(path) {
		return runtime, errors.New("native Lite runtime path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return runtime, errors.New("native Lite runtime must be an ordinary 0600 file")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) < 2 || len(raw) > 4096 || json.Unmarshal(raw, &runtime) != nil {
		return realNativeLiteRuntime{}, errors.New("native Lite runtime JSON unavailable")
	}
	host, port, err := net.SplitHostPort(runtime.Endpoint)
	if err != nil || host != "127.0.0.1" || port == "" ||
		runtime.SchemaVersion != "sea.search.native-lite-runtime.v2" ||
		runtime.Owner != realNativeLiteOwner || runtime.Engine != "lite" ||
		runtime.MilvusLite != "3.2.1" || runtime.FAISSVersion != "1.15.0" ||
		!realNativeHash(runtime.EnginePackageSHA256) ||
		runtime.EnginePackageSHA256 != realNativeLitePackageSHA ||
		!filepath.IsAbs(runtime.Directory) || filepath.Dir(path) != runtime.Directory ||
		runtime.ReleasePath != filepath.Join(runtime.Directory, "release") {
		return realNativeLiteRuntime{}, errors.New("native Lite owner/version/endpoint differs")
	}
	directory, err := os.Lstat(runtime.Directory)
	if err != nil || !directory.IsDir() || directory.Mode().Perm() != 0700 {
		return realNativeLiteRuntime{}, errors.New("native Lite directory is not private")
	}
	runtime.RawSHA256 = object.Hash(raw)
	return runtime, nil
}

func validRealNativeSettings(raw []byte) bool {
	if len(raw) < 2 || len(raw) > 32<<10 {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 6 {
		return false
	}
	for _, key := range []string{"schema_version", "engine", "namespace",
		"sparse_backend", "dense", "multivector"} {
		if fields[key] == nil {
			return false
		}
	}
	for _, key := range []string{"dense", "multivector"} {
		var nested map[string]json.RawMessage
		if json.Unmarshal(fields[key], &nested) != nil || len(nested) != 3 ||
			nested["m"] == nil || nested["ef_construction"] == nil || nested["ef_search"] == nil {
			return false
		}
	}
	var settings struct {
		SchemaVersion string `json:"schema_version"`
		Engine        string `json:"engine"`
		Namespace     string `json:"namespace"`
		SparseBackend string `json:"sparse_backend"`
		Dense         struct {
			M              int `json:"m"`
			EFConstruction int `json:"ef_construction"`
			EFSearch       int `json:"ef_search"`
		} `json:"dense"`
		MultiVector struct {
			M              int `json:"m"`
			EFConstruction int `json:"ef_construction"`
			EFSearch       int `json:"ef_search"`
		} `json:"multivector"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&settings) != nil || decoder.Decode(new(any)) != io.EOF {
		return false
	}
	return settings.SchemaVersion == "sea.search.native-backends.v2" &&
		settings.Engine == "lite" && settings.SparseBackend == "frozen_ip_postings" &&
		regexp.MustCompile(`^native_[a-f0-9]{12}$`).MatchString(settings.Namespace) &&
		settings.Dense.M == 16 && settings.Dense.EFConstruction == 128 &&
		settings.Dense.EFSearch == 64 && settings.MultiVector.M == 16 &&
		settings.MultiVector.EFConstruction == 128 && settings.MultiVector.EFSearch == 64
}

func realNativeSettingsJCSSHA256(raw []byte) (string, error) {
	if !validRealNativeSettings(raw) {
		return "", errors.New("Native Hybrid settings are not the six literal learned-IP fields")
	}
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return "", err
	}
	return object.Hash(canonical), nil
}

func writeNativeOnce(path string, raw []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// Native child outputs are kept outside t.TempDir when the task acceptance
// script asks for evidence; configured credentials remain in 0600 files.
func persistNativeTestBytes(t *testing.T, name string, raw []byte) {
	t.Helper()
	base := os.Getenv("KNOWLEDGE_OBS_EVIDENCE_DIR")
	if base == "" {
		return
	}
	if !filepath.IsAbs(base) || strings.Contains(name, "/") {
		t.Fatal("Native evidence path is not a bounded task directory/name")
	}
	dir := filepath.Join(base, "native-four-source")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeNativeOnce(filepath.Join(dir, name), raw); err != nil {
		t.Fatal(err)
	}
}

// This owner is started before BGE Build and released by a cleanup registered
// before the formal API cleanup, so one Lite process outlives product search.
func startRealBTWNativeLite(t *testing.T, parentDir, btwRoot string) (realNativeLiteRuntime, string) {
	t.Helper()
	pythonPath := os.Getenv("SEA_RTW_NATIVE_PYTHON")
	if !filepath.IsAbs(pythonPath) {
		t.Fatal("test native Lite requires one pinned absolute Python interpreter")
	}
	liteDir := filepath.Join(parentDir, "native-lite")
	if err := os.Mkdir(liteDir, 0700); err != nil {
		t.Fatal(err)
	}
	// The Python owner resolves the directory through TMPDIR symlinks
	// (/var/folders -> /private/var/folders) before publishing runtime.json;
	// compare receipts against the same resolved path.
	if resolved, resolveErr := filepath.EvalSymlinks(liteDir); resolveErr == nil {
		liteDir = resolved
	}
	logPath := filepath.Join(parentDir, "native-lite-owner.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(btwRoot, "cmd", "api", "native_lite_owner.py")
	if info, err := os.Lstat(script); err != nil || !info.Mode().IsRegular() {
		logFile.Close()
		t.Fatal("fixed BTW native Lite owner script missing")
	}
	cmd := exec.Command(pythonPath, script, "--directory", liteDir)
	cmd.Dir = btwRoot
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatal(err)
	}
	exited := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(exited)
	}()
	var runtime realNativeLiteRuntime
	var runtimePath string
	t.Cleanup(func() {
		release := filepath.Join(liteDir, "release")
		if err := os.WriteFile(release, []byte("test product completed\n"), 0600); err != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			t.Errorf("native Lite release marker unavailable: %v", err)
		}
		select {
		case <-exited:
			if waitErr != nil {
				t.Errorf("native Lite owner exited unsuccessfully: %v; log=%s", waitErr, logPath)
			}
		case <-time.After(30 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
			t.Error("native Lite owner failed to stop after release")
		}
		logFile.Close()
		if logRaw, err := os.ReadFile(logPath); err == nil {
			persistNativeTestBytes(t, "native-lite-owner.log", logRaw)
		} else {
			t.Errorf("native Lite owner log unavailable after stop: %v", err)
		}
		for _, name := range []string{"runtime.json", "stop.json"} {
			if raw, err := os.ReadFile(filepath.Join(liteDir, name)); err == nil {
				persistNativeTestBytes(t, "native-lite-"+name, raw)
			} else {
				t.Errorf("native Lite %s bytes unavailable after stop: %v", name, err)
			}
		}
		stopInfo, stopErr := os.Lstat(filepath.Join(liteDir, "stop.json"))
		if stopErr != nil || !stopInfo.Mode().IsRegular() || stopInfo.Mode().Perm() != 0600 {
			t.Error("native Lite owner omitted private stop receipt")
		} else {
			var stopped struct {
				Owner               string `json:"owner"`
				Stopped             bool   `json:"stopped"`
				Endpoint            string `json:"endpoint"`
				EnginePackageSHA256 string `json:"engine_package_sha256"`
				FAISSVersion        string `json:"faiss_version"`
			}
			raw, err := os.ReadFile(filepath.Join(liteDir, "stop.json"))
			if err != nil || json.Unmarshal(raw, &stopped) != nil ||
				stopped.Owner != realNativeLiteOwner || !stopped.Stopped ||
				stopped.Endpoint != runtime.Endpoint ||
				stopped.EnginePackageSHA256 != runtime.EnginePackageSHA256 ||
				stopped.FAISSVersion != runtime.FAISSVersion {
				t.Error("native Lite stop receipt belongs to another owner or endpoint")
			}
		}
		if conn, err := net.DialTimeout("tcp", runtime.Endpoint, 250*time.Millisecond); err == nil {
			conn.Close()
			t.Error("native Lite listener remained reachable after process exit")
		}
	})
	for deadline := time.Now().Add(120 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		select {
		case <-exited:
			logFile.Close()
			t.Fatalf("native Lite owner exited before runtime: %v; log=%s", waitErr, logPath)
		default:
		}
		runtimePath = filepath.Join(liteDir, "runtime.json")
		runtime, err = readRealNativeLiteRuntime(runtimePath)
		if err == nil {
			if conn, dialErr := net.DialTimeout("tcp", runtime.Endpoint, time.Second); dialErr == nil {
				conn.Close()
				return runtime, runtimePath
			}
		}
	}
	t.Fatalf("native Lite owner did not publish a valid live runtime; log=%s", logPath)
	return realNativeLiteRuntime{}, ""
}
