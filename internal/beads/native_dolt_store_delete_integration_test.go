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

// severWispForeignKeys drops every foreign key on the wisp tables, the state
// CALL DOLT_RESET('--hard') leaves dolt_ignored tables in.
func severWispForeignKeys(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`SELECT TABLE_NAME, CONSTRAINT_NAME FROM information_schema.TABLE_CONSTRAINTS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME LIKE 'wisp%' AND CONSTRAINT_TYPE = 'FOREIGN KEY'`)
	if err != nil {
		t.Fatalf("list wisp foreign keys: %v", err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup
	var keys [][2]string
	for rows.Next() {
		var table, constraint string
		if err := rows.Scan(&table, &constraint); err != nil {
			t.Fatalf("scan wisp foreign key: %v", err)
		}
		keys = append(keys, [2]string{table, constraint})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list wisp foreign keys: %v", err)
	}
	if len(keys) == 0 {
		t.Fatal("no wisp foreign keys to sever")
	}
	for _, k := range keys {
		if _, err := db.Exec("ALTER TABLE `" + k[0] + "` DROP FOREIGN KEY `" + k[1] + "`"); err != nil {
			t.Fatalf("drop %s.%s: %v", k[0], k[1], err)
		}
	}
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
}

func TestNativeDoltStoreDeleteWispRemovesChildRowsWithoutForeignKeys(t *testing.T) {
	store, db := openServerNativeDoltStoreWithDB(t)
	severWispForeignKeys(t, db)
	id := createWispWithChildRows(t, store, db)
	if err := store.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	assertNoChildRows(t, db, id)
}
