package entitlement

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProductionBuildExcludesEntitlementSeed(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	backendDir := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	cmd := exec.Command("go", "list", "-tags=production", "-json", "./internal/entitlement")
	cmd.Dir = backendDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("listing production entitlement package: %v", err)
	}
	var listed struct{ GoFiles []string }
	if err := json.Unmarshal(out, &listed); err != nil {
		t.Fatalf("decoding go list: %v", err)
	}
	for _, name := range listed.GoFiles {
		if name == "seed_nonprod.go" {
			t.Fatal("production entitlement package includes the non-production seed")
		}
	}
	for _, name := range listed.GoFiles {
		if strings.Contains(strings.ToLower(name), "seed") {
			t.Fatalf("production entitlement package exposes seed source %q", name)
		}
	}
	build := exec.Command("go", "build", "-tags=production", "-o", filepath.Join(t.TempDir(), "gradex-entitlement.a"), "./internal/entitlement")
	build.Dir = backendDir
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building production entitlement package: %v\n%s", err, output)
	}
	apiBuild := exec.Command("go", "build", "-tags=production", "-o", filepath.Join(t.TempDir(), "gradex-api"), "./cmd/api")
	apiBuild.Dir = backendDir
	if output, err := apiBuild.CombinedOutput(); err != nil {
		t.Fatalf("building production API composition: %v\n%s", err, output)
	}

	assertNoExportedMintingSymbol(t, backendDir, listed.GoFiles)
	assertNoProductionMintingSurface(t, backendDir)
}

func assertNoExportedMintingSymbol(t *testing.T, backendDir string, files []string) {
	t.Helper()
	for _, name := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(backendDir, "internal/entitlement", name), nil, 0)
		if err != nil {
			t.Fatalf("parsing production entitlement source %s: %v", name, err)
		}
		for _, declaration := range parsed.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			for _, forbidden := range []string{"Create", "Grant", "Issue", "Mint", "Insert", "Save", "Approve"} {
				if strings.HasPrefix(fn.Name.Name, forbidden) {
					t.Fatalf("production entitlement package exports minting symbol %s", fn.Name.Name)
				}
			}
		}
	}
}

func assertNoProductionMintingSurface(t *testing.T, backendDir string) {
	t.Helper()
	for _, relative := range []string{"cmd", "internal/httpapi", "internal/config"} {
		root := filepath.Join(backendDir, relative)
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			bytes, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			content := string(bytes)
			for _, forbidden := range []string{"INSERT INTO entitlements", "\"POST /entitlements", "\"PUT /entitlements", "CreateEntitlement", "GrantEntitlement", "MintEntitlement"} {
				if strings.Contains(content, forbidden) {
					return &productionMintingSurfaceError{path: path, token: forbidden}
				}
			}
			return nil
		})
		if err != nil {
			var found *productionMintingSurfaceError
			if errors.As(err, &found) {
				t.Fatalf("production entitlement minting surface found in %s (%s)", found.path, found.token)
			}
			t.Fatalf("scanning %s for production entitlement surfaces: %v", relative, err)
		}
	}
}

type productionMintingSurfaceError struct {
	path  string
	token string
}

func (e *productionMintingSurfaceError) Error() string { return e.path + ": " + e.token }
