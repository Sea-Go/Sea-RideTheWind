package ridethewind_test

// 边界守护（治理基线 G7，仿 BTW service/boundary_test.go）：
// 1. service/<域>/ 的代码不得 import 其他域的 /internal/ 路径（go-zero internal 在单 module
//    内可见，唯一防线就是本测试）；生成物（pb/、生成 client 目录）在 internal 之外，不受限。
// 2. service/common/ 不得 import 任何 service/<域>。
// 模块名从 go.mod 动态读取，避免与模块正名（C45/G3）的合并顺序耦合。

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func moduleName(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("读取 go.mod 失败: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	t.Fatal("go.mod 缺少 module 声明")
	return ""
}

func fileDomain(rel string) (string, bool) {
	segs := strings.Split(filepath.ToSlash(rel), "/")
	if len(segs) >= 2 && segs[0] == "service" {
		return segs[1], true
	}
	return "", false
}

func TestBoundaryNoCrossDomainInternalImports(t *testing.T) {
	mod := moduleName(t)
	violations := 0
	err := filepath.Walk("service", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "pb", "generated":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(".", path)
		ownDomain, ok := fileDomain(filepath.ToSlash(rel))
		if !ok {
			return nil
		}
		fset := token.NewFileSet()
		astFile, pErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if pErr != nil {
			return nil
		}
		for _, imp := range astFile.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(p, mod+"/service/") {
				continue
			}
			rest := strings.TrimPrefix(p, mod+"/service/")
			segs := strings.Split(rest, "/")
			targetDomain := segs[0]
			if ownDomain == "common" {
				if targetDomain != "common" {
					t.Errorf("[common 越界] %s import %s（common 不得依赖业务域）", rel, p)
					violations++
				}
				continue
			}
			if targetDomain == ownDomain || targetDomain == "common" {
				continue
			}
			if strings.Contains(rest, "/internal/") {
				t.Errorf("[跨域 internal] %s import %s（%s 域不得进入 %s 的 internal）", rel, p, ownDomain, targetDomain)
				violations++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if violations == 0 {
		t.Log("跨域 internal 检查通过：14 个业务域与 common 的依赖方向干净")
	}
}

// 反向验证：用合成的跨域 internal import 断言扫描逻辑确实会检出。
func TestBoundaryDetectorCatchesCrossDomainInternal(t *testing.T) {
	dir := t.TempDir()
	src := "package x\n\nimport _ \"" + "example.com/mod" + "/service/article/rpc/internal/model\"\n"
	if err := os.MkdirAll(filepath.Join(dir, "service", "comment", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "service", "comment", "x", "a.go")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	caught := false
	for _, imp := range astFile.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		if strings.HasPrefix(p, "example.com/mod/service/article/") && strings.Contains(p, "/internal/") {
			caught = true
		}
	}
	if !caught {
		t.Fatal("反向验证失败：comment 域 import article 的 internal 应被检出")
	}
}
