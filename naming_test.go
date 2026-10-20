package ridethewind_test

// C45 唯一正名（Sea-Docs《规划决策与待对齐事项》C45 与《工程目录与工作规范》§3.7）：
// 目录名、Go 包名不得携带 v2/v3 等代次后缀。协议版本（事件/RPC 契约字符串）不在范围。
// 白名单 naming_allowlist.json，每条豁免含 {path, reason, converge_event}，收敛后删除即恢复检查。

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var generationSuffixRe = regexp.MustCompile(`v[2-9][0-9]*$`)

var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "pb": true,
	"generated": true, "dist": true, "target": true, "build": true,
	".manage-logs": true, ".gocache": true,
}

type allowlistEntry struct {
	Path          string `json:"path"`
	Package       string `json:"package"`
	Reason        string `json:"reason"`
	ConvergeEvent string `json:"converge_event"`
}

type allowlistFile struct {
	Entries []allowlistEntry `json:"entries"`
}

type namingViolation struct {
	Path string
	Kind string
	Msg  string
}

func loadAllowlist(root string) (map[string]bool, error) {
	raw, err := os.ReadFile(filepath.Join(root, "naming_allowlist.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	var al allowlistFile
	if err := json.Unmarshal(raw, &al); err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, e := range al.Entries {
		if e.Path == "" || e.Reason == "" || e.ConvergeEvent == "" {
			return nil, os.ErrInvalid
		}
		allowed[e.Path] = true
	}
	return allowed, nil
}

func isAllowed(allowed map[string]bool, path string) bool {
	for p := path; p != "." && p != string(filepath.Separator); p = filepath.Dir(p) {
		if allowed[filepath.ToSlash(p)] {
			return true
		}
	}
	return false
}

func scanNamingRoot(root string) ([]namingViolation, error) {
	allowed, err := loadAllowlist(root)
	if err != nil {
		return nil, err
	}
	var violations []namingViolation
	seenPkgs := map[string]bool{}

	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if info.IsDir() {
			if rel == "." {
				return nil
			}
			if skippedDirs[info.Name()] {
				return filepath.SkipDir
			}
			if generationSuffixRe.MatchString(info.Name()) {
				violations = append(violations, namingViolation{filepath.ToSlash(rel), "dir", "目录名携带代次后缀（C45）"})
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.Contains(filepath.ToSlash(rel), "/pb/") || strings.Contains(filepath.ToSlash(rel), "/generated/") {
			return nil
		}
		fset := token.NewFileSet()
		astFile, pErr := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
		if pErr != nil {
			return nil
		}
		pkg := strings.TrimSuffix(astFile.Name.Name, "_test")
		if generationSuffixRe.MatchString(pkg) && !seenPkgs[pkg+filepath.ToSlash(filepath.Dir(rel))] {
			seenPkgs[pkg+filepath.ToSlash(filepath.Dir(rel))] = true
			violations = append(violations, namingViolation{filepath.ToSlash(rel), "package", "包名携带代次后缀（C45）: " + pkg})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	filtered := violations[:0]
	for _, v := range violations {
		if !isAllowed(allowed, v.Path) {
			filtered = append(filtered, v)
		}
	}
	return filtered, nil
}

func TestNamingRepoHasNoGenerationSuffix(t *testing.T) {
	violations, err := scanNamingRoot(".")
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	for _, v := range violations {
		t.Errorf("[%s] %s: %s", v.Kind, v.Path, v.Msg)
	}
}

// 反向验证（§3.7 第 4 条）：无白名单时违例必须现形；白名单只压制其路径前缀下的违例。
func TestNamingWhitelistSuppressesAndReverseVerifies(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"foo_v2", "bar"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "bar", "x.go"), []byte("package bar_v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	without, err := scanNamingRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(without) != 2 {
		t.Fatalf("反向验证失败：无白名单应检出 2 处，实际 %d: %+v", len(without), without)
	}

	if err := os.WriteFile(filepath.Join(root, "naming_allowlist.json"), []byte(`{"entries":[{"path":"foo_v2","reason":"r","converge_event":"e"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	with, err := scanNamingRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(with) != 1 || !strings.HasSuffix(with[0].Path, "bar/x.go") {
		t.Fatalf("白名单应只压制 foo_v2 前缀，实际剩余: %+v", with)
	}
}
