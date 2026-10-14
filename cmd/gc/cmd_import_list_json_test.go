package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/fsys"
	"github.com/gastownhall/gascity/internal/packman"
)

// writeImportListFixtureCity declares a locked remote, an unlocked remote,
// and a path import, with packs.lock carrying one transitive pack.
func writeImportListFixtureCity(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeCityToml(t, dir, "[workspace]\nname = \"demo\"\n")
	writePackToml(t, dir, `[pack]
name = "demo"
schema = 1

[imports.tools]
source = "https://example.com/tools.git"
version = "^1.4"

[imports.unlocked]
source = "https://example.com/unlocked.git"
version = "^9.9"

[imports.local]
source = "../packs/local"
`)
	if err := packman.WriteLockfile(fsys.OSFS{}, dir, &packman.Lockfile{
		Schema: packman.LockfileSchema,
		Packs: map[string]packman.LockedPack{
			"https://example.com/tools.git": {Version: "1.4.2", Commit: "aaaa"},
			"https://example.com/base.git":  {Version: "2.0.0", Commit: "bbbb"},
		},
	}); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	return dir
}

func TestImportListJSONMatchesTextColumns(t *testing.T) {
	clearGCEnv(t)
	dir := writeImportListFixtureCity(t)

	var text, textErr bytes.Buffer
	if code := run([]string{"--city", dir, "import", "list"}, &text, &textErr); code != 0 {
		t.Fatalf("run(import list) = %d, stderr=%q", code, textErr.String())
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--city", dir, "import", "list", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(import list --json) = %d, stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	validateJSONAgainstResultSchema(t, []string{"import", "list"}, stdout.Bytes())
	assertTopLevelOKTrue(t, stdout.Bytes())

	var doc importListJSON
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("import list output is not JSON: %v\n%s", err, stdout.String())
	}
	want := []importListRow{
		{Binding: "local", Source: "../packs/local", LockState: "path"},
		{Binding: "tools", Source: "https://example.com/tools.git", Version: "^1.4", LockVersion: "1.4.2", LockState: "locked"},
		{Binding: "unlocked", Source: "https://example.com/unlocked.git", Version: "^9.9", LockState: "unlocked"},
		{Transitive: true, Source: "https://example.com/base.git", LockVersion: "2.0.0", LockState: "locked"},
	}
	if len(doc.Imports) != len(want) {
		t.Fatalf("imports = %+v, want %+v", doc.Imports, want)
	}
	for i := range want {
		if doc.Imports[i] != want[i] {
			t.Errorf("imports[%d] = %+v, want %+v", i, doc.Imports[i], want[i])
		}
	}

	lines := strings.Split(strings.TrimSuffix(text.String(), "\n"), "\n")
	if len(lines) != len(doc.Imports) {
		t.Fatalf("text rows = %d, json rows = %d\ntext:\n%s", len(lines), len(doc.Imports), text.String())
	}
	for i, line := range lines {
		cols := strings.Split(line, "\t")
		row := doc.Imports[i]
		binding := row.Binding
		if row.Transitive {
			binding = "(transitive)"
		}
		lockColumn := row.LockVersion
		if row.LockState != "locked" {
			lockColumn = "(" + row.LockState + ")"
		}
		got := []string{binding, row.Source, row.Version, lockColumn}
		if strings.Join(cols, "\t") != strings.Join(got, "\t") {
			t.Errorf("row %d: text %q, json %q", i, cols, got)
		}
	}
}

func TestImportListJSONRejectsTree(t *testing.T) {
	clearGCEnv(t)
	dir := writeImportListFixtureCity(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--city", dir, "import", "list", "--tree", "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("run(import list --tree --json) = 0, want nonzero; stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--json is not supported with --tree") {
		t.Fatalf("stderr = %q, want --tree diagnostic", stderr.String())
	}
}
