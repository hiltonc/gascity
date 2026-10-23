package main

import (
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
	"github.com/gastownhall/gascity/internal/packman"
)

// importListSchemaVersion identifies the document emitted by
// "gc import list --json".
const importListSchemaVersion = "1"

// Lock states of an import list row.
const (
	importListLockLocked   = "locked"
	importListLockUnlocked = "unlocked"
	importListLockPath     = "path"
)

// importListJSON is the "gc import list --json" document. Its rows are
// the rows "gc import list" prints, in the same order.
type importListJSON struct {
	SchemaVersion string          `json:"schema_version"`
	OK            bool            `json:"ok"`
	Imports       []importListRow `json:"imports"`
}

// importListRow is one listed import: a direct binding, or a pack the
// direct imports pull in transitively.
type importListRow struct {
	// Binding is the import binding name; empty for transitive rows.
	Binding string `json:"binding"`
	// Transitive is true for packs reached only through another import.
	Transitive bool `json:"transitive"`
	// Source is the import source as declared, or the locked source for
	// transitive rows.
	Source string `json:"source"`
	// Version is the declared version constraint; empty for path imports
	// and transitive rows.
	Version string `json:"version"`
	// LockVersion is the packs.lock version; empty unless LockState is
	// "locked".
	LockVersion string `json:"lock_version"`
	// LockState is "locked", "unlocked" (remote with no packs.lock entry),
	// or "path" (local directory import).
	LockState string `json:"lock_state"`
}

func doImportListJSON(cityPath string, stdout, stderr io.Writer) int {
	inspectImports, lock, err := loadImportListInputs(cityPath)
	if err != nil {
		fmt.Fprintf(stderr, "gc import list: %v\n", err) //nolint:errcheck
		return 1
	}
	rows, err := importListRowsFor(cityPath, inspectImports, lock)
	if err != nil {
		fmt.Fprintf(stderr, "gc import list: %v\n", err) //nolint:errcheck
		return 1
	}
	return writeCLIJSONLineOrExit(stdout, stderr, "gc import list", importListJSON{
		SchemaVersion: importListSchemaVersion,
		OK:            true,
		Imports:       rows,
	})
}

func loadImportListInputs(cityPath string) (map[string]config.Import, *packman.Lockfile, error) {
	scope, err := loadImportScopeFS(fsys.OSFS{}, cityPath)
	if err != nil {
		return nil, nil, err
	}
	lock, err := readImportLockfile(fsys.OSFS{}, cityPath)
	if err != nil {
		return nil, nil, err
	}
	inspectImports, err := collectInspectableImportsFS(fsys.OSFS{}, cityPath, scope)
	if err != nil {
		return nil, nil, err
	}
	return inspectImports, lock, nil
}

// importListRowsFor returns the direct imports sorted by binding, then
// their transitive packs sorted by source.
func importListRowsFor(cityPath string, inspectImports map[string]config.Import, lock *packman.Lockfile) ([]importListRow, error) {
	allImports, err := collectAllImportsFS(cityPath)
	if err != nil {
		return nil, err
	}
	allowLockOnlyFallback := len(allImports) == len(inspectImports)

	graph, graphErr := buildImportGraph(inspectImports, lock)
	if graphErr != nil && !allowLockOnlyFallback {
		return nil, graphErr
	}

	rows := []importListRow{}
	directSources := make(map[string]bool)
	for _, name := range slices.Sorted(maps.Keys(inspectImports)) {
		imp := inspectImports[name]
		row := importListRow{Binding: name, Source: imp.Source, Version: imp.Version}
		if !isRemoteImportSource(imp.Source) {
			row.LockState = importListLockPath
			rows = append(rows, row)
			continue
		}
		directSources[imp.Source] = true
		if pack, ok := lock.Packs[imp.Source]; ok {
			row.LockState = importListLockLocked
			row.LockVersion = pack.Version
		} else {
			row.LockState = importListLockUnlocked
		}
		rows = append(rows, row)
	}

	transitive := collectTransitiveImports(graph, directSources)
	if len(transitive) == 0 && allowLockOnlyFallback {
		for source, pack := range lock.Packs {
			if !directSources[source] {
				transitive[source] = pack
			}
		}
	}
	for _, source := range slices.Sorted(maps.Keys(transitive)) {
		rows = append(rows, importListRow{
			Transitive:  true,
			Source:      source,
			LockVersion: transitive[source].Version,
			LockState:   importListLockLocked,
		})
	}
	return rows, nil
}
