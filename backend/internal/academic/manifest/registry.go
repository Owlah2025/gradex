package manifest

import (
	"embed"
	"fmt"
	"path"
	"sort"

	"github.com/goccy/go-yaml"
)

// Manifests are embedded rather than read from disk at runtime. That is the
// mechanism that makes "the Admin selects a known checked-in manifest" true:
// there is no filesystem path and no URL a caller could supply, because the
// only readable manifests are the ones compiled into the binary.
//
//go:embed data/*/manifest.yaml data/*/sources.yaml
var files embed.FS

const dataRoot = "data"

// Package is one reviewable import unit: the catalog plus its provenance.
type Package struct {
	Manifest *Manifest
	Sources  *SourceCatalog
	// Directory is the embedded directory name, retained for evidence and
	// reporting. It is never taken from a caller.
	Directory string
}

// Available lists every embedded manifest identifier, sorted for determinism.
func Available() ([]string, error) {
	entries, err := files.ReadDir(dataRoot)
	if err != nil {
		return nil, fmt.Errorf("reading embedded manifests: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		loaded, err := loadDirectory(entry.Name())
		if err != nil {
			return nil, err
		}
		ids = append(ids, loaded.Manifest.ID)
	}
	sort.Strings(ids)
	return ids, nil
}

// Load resolves a manifest identifier to its embedded package and validates it.
// An unknown identifier is an error, never a fallback.
func Load(id string) (*Package, error) {
	entries, err := files.ReadDir(dataRoot)
	if err != nil {
		return nil, fmt.Errorf("reading embedded manifests: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		loaded, err := loadDirectory(entry.Name())
		if err != nil {
			return nil, err
		}
		if loaded.Manifest.ID != id {
			continue
		}
		if err := loaded.Manifest.Validate(loaded.Sources); err != nil {
			return nil, err
		}
		return loaded, nil
	}
	return nil, fmt.Errorf("unknown manifest %q", id)
}

func loadDirectory(directory string) (*Package, error) {
	manifestBytes, err := files.ReadFile(path.Join(dataRoot, directory, "manifest.yaml"))
	if err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", directory, err)
	}
	sourceBytes, err := files.ReadFile(path.Join(dataRoot, directory, "sources.yaml"))
	if err != nil {
		return nil, fmt.Errorf("reading sources %s: %w", directory, err)
	}
	var parsed Manifest
	if err := yaml.UnmarshalWithOptions(manifestBytes, &parsed, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", directory, err)
	}
	var sources SourceCatalog
	if err := yaml.UnmarshalWithOptions(sourceBytes, &sources, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("parsing sources %s: %w", directory, err)
	}
	return &Package{Manifest: &parsed, Sources: &sources, Directory: directory}, nil
}

// ParsePackage validates already-decoded data. Tests use it to exercise
// validation against hand-built fixtures without embedding them.
func ParsePackage(manifestYAML, sourcesYAML []byte) (*Package, error) {
	var parsed Manifest
	if err := yaml.UnmarshalWithOptions(manifestYAML, &parsed, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	var sources SourceCatalog
	if err := yaml.UnmarshalWithOptions(sourcesYAML, &sources, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("parsing sources: %w", err)
	}
	pkg := &Package{Manifest: &parsed, Sources: &sources}
	if err := parsed.Validate(&sources); err != nil {
		return pkg, err
	}
	return pkg, nil
}
