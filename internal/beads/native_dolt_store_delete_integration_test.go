//go:build integration

package beads

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	beadslib "github.com/steveyegge/beads"
)

const wispChildRowsTestDatabase = "beads"

func openServerNativeDoltStoreWithDB(t *testing.T) (*NativeDoltStore, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	scopeRoot := t.TempDir()
	port := startTestDoltServer(t)
	beadsDir := filepath.Join(scopeRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("create .beads directory: %v", err)
	}
	metadata := fmt.Sprintf(`{"backend":"dolt","database":%q,"dolt_mode":"server","dolt_server_host":"127.0.0.1","dolt_server_port":%d}`, wispChildRowsTestDatabase, port)
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(metadata), 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	storage, err := beadslib.OpenBestAvailable(ctx, beadsDir)
	if err != nil {
		t.Skipf("upstream native beads storage unavailable: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	if err := storage.SetConfig(ctx, "issue_prefix", "gc"); err != nil {
		t.Fatalf("set issue prefix: %v", err)
	}
	accessor, ok := storage.(testRawDBGetter)
	if !ok {
		t.Skip("storage does not expose a raw DB")
	}
	return newNativeDoltStoreWithStorageAndPrefix(storage, "wisp-child-rows", "gc"), accessor.DB()
}

// createWispWithChildRows creates a wisp that has label, event and comment
// rows, the shape order-tracking wisps leave behind.
func createWispWithChildRows(t *testing.T, store *NativeDoltStore, db *sql.DB) string {
	t.Helper()
	wisp, err := store.Create(Bead{Title: "tracking wisp", Ephemeral: true, Labels: []string{"order-tracking", "exec"}})
	if err != nil {
		t.Fatalf("Create wisp: %v", err)
	}
	if err := store.SetMetadata(wisp.ID, "k", "v"); err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}
	if _, err := db.Exec("INSERT INTO wisp_comments (id, issue_id, author, text) VALUES (UUID(), ?, 'test', 'note')", wisp.ID); err != nil {
		t.Fatalf("seed wisp comment: %v", err)
	}
	for _, table := range []string{"wisp_labels", "wisp_events", "wisp_comments"} {
		if n := countChildRows(t, db, table, wisp.ID); n == 0 {
			t.Fatalf("%s rows for %s = 0, want seeded rows", table, wisp.ID)
		}
	}
	return wisp.ID
}

func countChildRows(t *testing.T, db *sql.DB, table, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM `"+table+"` WHERE issue_id = ?", id).Scan(&n); err != nil {
		t.Fatalf("count %s rows for %s: %v", table, id, err)
	}
	return n
}

func assertNoChildRows(t *testing.T, db *sql.DB, ids ...string) {
	t.Helper()
	for _, id := range ids {
		for _, table := range []string{"wisp_labels", "wisp_events", "wisp_comments", "wisp_dependencies"} {
			if n := countChildRows(t, db, table, id); n != 0 {
				t.Errorf("%s rows for deleted wisp %s = %d, want 0", table, id, n)
			}
		}
	}
}

// dropWispChildCascades reproduces a database whose wisp child tables lost
// their ON DELETE CASCADE foreign keys.
func dropWispChildCascades(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, c := range WispChildTables {
		if c.Table == "wisp_dependencies" {
			continue
		}
		if _, err := db.Exec("ALTER TABLE `" + c.Table + "` DROP FOREIGN KEY `" + c.FK + "`"); err != nil {
			t.Fatalf("drop %s: %v", c.FK, err)
		}
	}
}

func inspectWispChildRows(t *testing.T, db *sql.DB) map[string]WispChildTableState {
	t.Helper()
	states, err := InspectWispChildRows(context.Background(), db, wispChildRowsTestDatabase)
	if err != nil {
		t.Fatalf("InspectWispChildRows: %v", err)
	}
	byTable := make(map[string]WispChildTableState, len(states))
	for _, s := range states {
		byTable[s.Table] = s
	}
	if len(byTable) != len(WispChildTables) {
		t.Fatalf("InspectWispChildRows returned %d tables, want %d: %+v", len(byTable), len(WispChildTables), states)
	}
	return byTable
}

func TestNativeDoltStoreDeleteWispRemovesChildRows(t *testing.T) {
	store, db := openServerNativeDoltStoreWithDB(t)
	id := createWispWithChildRows(t, store, db)
	if err := store.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	assertNoChildRows(t, db, id)
}

// The retention and nudge/mail sweeps prune by deleting each wisp in turn.
func TestNativeDoltStoreBulkDeleteWispsRemovesChildRows(t *testing.T) {
	store, db := openServerNativeDoltStoreWithDB(t)
	ids := make([]string, 5)
	for i := range ids {
		ids[i] = createWispWithChildRows(t, store, db)
	}
	for _, id := range ids {
		if err := store.Delete(id); err != nil {
			t.Fatalf("Delete %s: %v", id, err)
		}
	}
	assertNoChildRows(t, db, ids...)
	for table, s := range inspectWispChildRows(t, db) {
		if !s.Healthy() {
			t.Errorf("%s = %+v, want healthy", table, s)
		}
	}
}

func TestRepairWispChildRowsDeletesOrphansAndRestoresCascade(t *testing.T) {
	store, db := openServerNativeDoltStoreWithDB(t)
	dropWispChildCascades(t, db)
	kept := createWispWithChildRows(t, store, db)
	orphaned := make([]string, 3)
	for i := range orphaned {
		orphaned[i] = createWispWithChildRows(t, store, db)
		if err := store.Delete(orphaned[i]); err != nil {
			t.Fatalf("Delete %s: %v", orphaned[i], err)
		}
	}

	before := inspectWispChildRows(t, db)
	for _, table := range []string{"wisp_labels", "wisp_events", "wisp_comments"} {
		s := before[table]
		if s.Orphans == 0 || s.HasCascade {
			t.Errorf("%s before repair = %+v, want orphans and no cascade", table, s)
		}
	}
	if s := before["wisp_dependencies"]; !s.Healthy() {
		t.Errorf("wisp_dependencies before repair = %+v, want healthy", s)
	}

	deleted, err := RepairWispChildRows(context.Background(), db, wispChildRowsTestDatabase, 2)
	if err != nil {
		t.Fatalf("RepairWispChildRows: %v", err)
	}
	for _, table := range []string{"wisp_labels", "wisp_events", "wisp_comments"} {
		if deleted[table] != before[table].Orphans {
			t.Errorf("deleted %s = %d, want %d", table, deleted[table], before[table].Orphans)
		}
	}
	for table, s := range inspectWispChildRows(t, db) {
		if !s.Healthy() {
			t.Errorf("%s after repair = %+v, want healthy", table, s)
		}
	}
	if n := countChildRows(t, db, "wisp_labels", kept); n != 2 {
		t.Errorf("wisp_labels rows for live wisp %s = %d, want 2", kept, n)
	}

	if err := store.Delete(kept); err != nil {
		t.Fatalf("Delete %s after repair: %v", kept, err)
	}
	assertNoChildRows(t, db, kept)
}

func TestInspectWispChildRowsSkipsDatabaseWithoutWisps(t *testing.T) {
	_, db := openServerNativeDoltStoreWithDB(t)
	if _, err := db.Exec("CREATE DATABASE no_wisps"); err != nil {
		t.Fatalf("create database: %v", err)
	}
	states, err := InspectWispChildRows(context.Background(), db, "no_wisps")
	if err != nil {
		t.Fatalf("InspectWispChildRows: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("InspectWispChildRows = %+v, want none", states)
	}
}
