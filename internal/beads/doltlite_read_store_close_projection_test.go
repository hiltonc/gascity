//go:build gascity_native_beads

package beads

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDoltliteReadStoreServesCloseAndAttribution pins the direct-SQL read path
// against a schema that carries bd's close and attribution columns. This store
// bypasses the bd CLI, so it has to project the four columns itself or a city
// on the native backend serves a narrower bead than the same city on bd.
func TestDoltliteReadStoreServesCloseAndAttribution(t *testing.T) {
	store, closeStore := newCloseColumnDoltliteReadStore(t, true)
	defer closeStore()

	closed, err := store.Get("gc-closed")
	if err != nil {
		t.Fatalf("Get(gc-closed): %v", err)
	}
	wantClosedAt := time.Date(2026, 9, 17, 17, 47, 39, 0, time.UTC)
	if closed.ClosedAt == nil {
		t.Fatal("ClosedAt = nil, want the closed_at column")
	}
	if !closed.ClosedAt.Equal(wantClosedAt) {
		t.Fatalf("ClosedAt = %s, want %s", closed.ClosedAt, wantClosedAt)
	}
	if closed.CloseReason != "shipped in #118" {
		t.Fatalf("CloseReason = %q, want %q", closed.CloseReason, "shipped in #118")
	}
	if closed.Owner != "me@heyhilton.com" {
		t.Fatalf("Owner = %q, want %q", closed.Owner, "me@heyhilton.com")
	}
	if closed.CreatedBy != "gc-mayor" {
		t.Fatalf("CreatedBy = %q, want %q", closed.CreatedBy, "gc-mayor")
	}

	open, err := store.Get("gc-open")
	if err != nil {
		t.Fatalf("Get(gc-open): %v", err)
	}
	if open.ClosedAt != nil {
		t.Fatalf("ClosedAt = %s, want nil for an open bead", open.ClosedAt)
	}
	if open.CloseReason != "" {
		t.Fatalf("CloseReason = %q, want empty for an open bead", open.CloseReason)
	}
	if open.CreatedBy != "gc-mayor" {
		t.Fatalf("CreatedBy = %q, want %q", open.CreatedBy, "gc-mayor")
	}
}

// TestDoltliteReadStoreReadsSnapshotsWithoutCloseColumns covers the snapshot
// this store has always had to tolerate: one written before those columns
// existed. The projection probes for each column rather than assuming it, so
// an older database reads as "no close data" instead of failing the query.
func TestDoltliteReadStoreReadsSnapshotsWithoutCloseColumns(t *testing.T) {
	store, closeStore := newCloseColumnDoltliteReadStore(t, false)
	defer closeStore()

	closed, err := store.Get("gc-closed")
	if err != nil {
		t.Fatalf("Get(gc-closed): %v", err)
	}
	if closed.ClosedAt != nil {
		t.Fatalf("ClosedAt = %s, want nil when the column is absent", closed.ClosedAt)
	}
	if closed.CloseReason != "" || closed.Owner != "" || closed.CreatedBy != "" {
		t.Fatalf("close/attribution = %q/%q/%q, want empty when the columns are absent",
			closed.CloseReason, closed.Owner, closed.CreatedBy)
	}
	if closed.Status != "closed" {
		t.Fatalf("Status = %q, want closed", closed.Status)
	}
}

// newCloseColumnDoltliteReadStore builds a minimal doltlite fixture holding one
// closed and one open bead. withCloseColumns selects between a current schema
// and a snapshot predating the close and attribution columns.
func newCloseColumnDoltliteReadStore(t *testing.T, withCloseColumns bool) (*DoltliteReadStore, func()) {
	t.Helper()
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir beads dir: %v", err)
	}
	meta := []byte(`{"backend":"doltlite","database":"doltlite","dolt_database":"hq"}`)
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), meta, 0o600); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	dbDir := filepath.Join(beadsDir, "doltlite")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir doltlite dir: %v", err)
	}
	dbPath := filepath.Join(dbDir, "hq.db")
	db, err := sql.Open("sqlite", dbPath+"?_busy_timeout=10000")
	if err != nil {
		t.Fatalf("open doltlite fixture db: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	rowColumns := `,
			ephemeral INTEGER DEFAULT 0,
			no_history INTEGER DEFAULT 0`
	if withCloseColumns {
		rowColumns += `,
			closed_at TEXT,
			close_reason TEXT,
			owner TEXT,
			created_by TEXT`
	}
	createTestDoltliteSchemaWithRowColumns(t, db, rowColumns)

	createdAt := time.Date(2026, 9, 17, 16, 0, 0, 0, time.UTC)
	insertTestDoltliteIssue(t, db, "issues", "labels", "dependencies", testDoltliteIssue{
		ID:        "gc-closed",
		Title:     "Closed bead",
		Status:    "closed",
		IssueType: "task",
		CreatedAt: createdAt,
	})
	insertTestDoltliteIssue(t, db, "issues", "labels", "dependencies", testDoltliteIssue{
		ID:        "gc-open",
		Title:     "Open bead",
		Status:    "open",
		IssueType: "task",
		CreatedAt: createdAt,
	})
	if withCloseColumns {
		exec := func(query string, args ...any) {
			t.Helper()
			if _, err := db.Exec(query, args...); err != nil {
				t.Fatalf("seed close columns: %v", err)
			}
		}
		exec(`UPDATE issues SET closed_at = ?, close_reason = ?, owner = ?, created_by = ? WHERE id = 'gc-closed'`,
			time.Date(2026, 9, 17, 17, 47, 39, 0, time.UTC).Format(time.RFC3339Nano),
			"shipped in #118",
			"me@heyhilton.com",
			"gc-mayor",
		)
		exec(`UPDATE issues SET created_by = ? WHERE id = 'gc-open'`, "gc-mayor")
	}

	backing := NewBdStore(dir, func(string, string, ...string) ([]byte, error) {
		t.Fatal("backing bd runner should not be called by doltlite read tests")
		return nil, nil
	})
	store, err := NewDoltliteReadStore(dir, backing)
	if err != nil {
		t.Fatalf("NewDoltliteReadStore: %v", err)
	}
	return store, func() { _ = store.CloseStore() }
}
