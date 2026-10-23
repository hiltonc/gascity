package main

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/doctor"
	"github.com/gastownhall/gascity/internal/doltpool"
)

func TestWispChildRowsCheckFindsAndRepairsOrphans(t *testing.T) {
	_, port, _, cleanup := startPasswordedDoltServer(t, "",
		"CREATE TABLE wisps (id VARCHAR(255) PRIMARY KEY)",
		"CREATE TABLE wisp_labels (issue_id VARCHAR(255) NOT NULL, label VARCHAR(255) NOT NULL, PRIMARY KEY (issue_id, label))",
		"INSERT INTO wisps VALUES ('w-live')",
		"INSERT INTO wisp_labels VALUES ('w-live', 'exec'), ('w-gone', 'exec'), ('w-gone', 'order-tracking'), ('w-gone-2', 'exec')",
	)
	defer cleanup()
	check := &wispChildRowsCheck{openDB: func() (*sql.DB, error) {
		return managedDoltOpenDB("127.0.0.1", fmt.Sprintf("%d", port), "root")
	}}

	res := check.Run(&doctor.CheckContext{})
	if res.Status != doctor.StatusWarning {
		t.Fatalf("Run status = %v (%s), want warning", res.Status, res.Message)
	}
	if res.Message != "3 orphaned wisp child row(s) and 1 table(s) without a cascade foreign key across 1 database(s)" {
		t.Errorf("Run message = %q", res.Message)
	}
	if len(res.Details) != 1 || !strings.HasSuffix(res.Details[0], ".wisp_labels: 3 orphaned row(s), missing ON DELETE CASCADE foreign key fk_wisp_labels_issue") {
		t.Errorf("Run details = %q", res.Details)
	}

	if err := check.Fix(&doctor.CheckContext{}); err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if res := check.Run(&doctor.CheckContext{}); res.Status != doctor.StatusOK {
		t.Fatalf("Run after Fix status = %v (%s %q), want OK", res.Status, res.Message, res.Details)
	}

	database := strings.SplitN(res.Details[0], ".", 2)[0]
	scoped, err := doltpool.Open("127.0.0.1", fmt.Sprintf("%d", port), "root", "secret", database)
	if err != nil {
		t.Fatalf("open %s: %v", database, err)
	}
	if _, err := scoped.Exec("DELETE FROM wisps WHERE id = 'w-live'"); err != nil {
		t.Fatalf("delete live wisp: %v", err)
	}
	var n int
	if err := scoped.QueryRow("SELECT COUNT(*) FROM wisp_labels").Scan(&n); err != nil {
		t.Fatalf("count labels: %v", err)
	}
	if n != 0 {
		t.Errorf("wisp_labels rows after deleting the last wisp = %d, want 0", n)
	}
}
