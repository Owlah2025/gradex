package learning

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// T018: learning is a consumer of Enrollment and Entitlement authority. Its
// public surface may resolve either one, but may never offer an authority-
// creating or lifecycle-mutating operation.
func TestLearningExportsNoEnrollmentOrEntitlementWriter(t *testing.T) {
	backendDir, files := listedPackageFiles(t, "./internal/learning", "")
	for _, name := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(backendDir, "internal/learning", name), nil, 0)
		if err != nil {
			t.Fatalf("parsing learning source %s: %v", name, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || !function.Name.IsExported() {
				continue
			}
			name := function.Name.Name
			if strings.Contains(name, "Enrollment") || strings.Contains(name, "Entitlement") {
				for _, forbidden := range []string{"Create", "Insert", "Update", "Delete", "Grant", "Mint", "Issue", "Save"} {
					if strings.HasPrefix(name, forbidden) {
						t.Fatalf("learning exports forbidden Enrollment or Entitlement writer %s", name)
					}
				}
			}
		}
	}
}

// T019: inspect every source file actually selected by the production build,
// then build the API composition. Test fixtures and the S4 non-production
// seed are absent from go list's GoFiles and cannot satisfy this assertion.
func TestProductionBuildHasNoEnrollmentCreationPath(t *testing.T) {
	backendDir, _ := listedPackageFiles(t, "./internal/learning", "production")
	cmd := exec.Command("go", "build", "-tags=production", "-o", filepath.Join(t.TempDir(), "gradex-api"), "./cmd/api")
	cmd.Dir = backendDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building production API composition: %v\n%s", err, output)
	}

	packages := listedProductionPackages(t, backendDir)
	for _, pkg := range packages {
		for _, name := range pkg.GoFiles {
			path := filepath.Join(pkg.Dir, name)
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading production source %s: %v", path, err)
			}
			if match := forbiddenCreation.FindString(string(contents)); match != "" {
				t.Fatalf("production creation or mutation path in %s: %q", path, match)
			}
			if s5CompositionPackage(pkg.ImportPath) && strings.Contains(string(contents), "FakeEntitlementChecker") {
				t.Fatalf("S5 production composition reaches legacy fake entitlement checker in %s", path)
			}
		}
	}
}

var forbiddenCreation = regexp.MustCompile(`(?is)\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+(?:enrollments|entitlements)\b|\b(?:Create|Insert|Update|Delete|Grant|Mint|Issue|Save)(?:Enrollment|Entitlement)\b`)

type listedPackage struct {
	ImportPath string
	Dir        string
	GoFiles    []string
}

func s5CompositionPackage(importPath string) bool {
	return strings.HasSuffix(importPath, "/cmd/api") ||
		strings.HasSuffix(importPath, "/internal/httpapi") ||
		strings.HasSuffix(importPath, "/internal/learning")
}

func listedPackageFiles(t *testing.T, packagePath, tags string) (string, []string) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	backendDir := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	args := []string{"list"}
	if tags != "" {
		args = append(args, "-tags="+tags)
	}
	args = append(args, "-json", packagePath)
	cmd := exec.Command("go", args...)
	cmd.Dir = backendDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("listing %s: %v", packagePath, err)
	}
	var pkg listedPackage
	if err := json.Unmarshal(output, &pkg); err != nil {
		t.Fatalf("decoding %s listing: %v", packagePath, err)
	}
	return backendDir, pkg.GoFiles
}

func listedProductionPackages(t *testing.T, backendDir string) []listedPackage {
	t.Helper()
	cmd := exec.Command("go", "list", "-tags=production", "-json", "./...")
	cmd.Dir = backendDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("listing production packages: %v", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	var packages []listedPackage
	for {
		var pkg listedPackage
		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decoding production package listing: %v", err)
		}
		packages = append(packages, pkg)
	}
	return packages
}
