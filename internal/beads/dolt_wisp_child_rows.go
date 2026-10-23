package beads

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// WispChildTable names a table whose rows belong to a wisp and the foreign key
// that deletes them with it.
type WispChildTable struct {
	Table  string
	Column string
	FK     string
}

// WispChildTables lists the wisp child tables. bd deletes a wisp's child rows
// only through these ON DELETE CASCADE foreign keys, so a database that lost
// one keeps every child row of every wisp deleted since.
var WispChildTables = []WispChildTable{
	{Table: "wisp_labels", Column: "issue_id", FK: "fk_wisp_labels_issue"},
	{Table: "wisp_events", Column: "issue_id", FK: "fk_wisp_events_issue"},
	{Table: "wisp_comments", Column: "issue_id", FK: "fk_wisp_comments_issue"},
	{Table: "wisp_dependencies", Column: "issue_id", FK: "fk_wisp_dep_issue"},
	{Table: "wisp_child_counters", Column: "parent_id", FK: "fk_wisp_child_counters_parent"},
}

// WispChildTableState is one child table's orphan count and cascade state.
type WispChildTableState struct {
	WispChildTable
	Orphans    int
	HasCascade bool
}

// Healthy reports whether the table has no orphans and deletes cascade into it.
func (s WispChildTableState) Healthy() bool { return s.Orphans == 0 && s.HasCascade }

// InspectWispChildRows reports the orphaned child rows and missing cascade
// foreign keys in database. It returns no states when database has no wisps
// table, and skips child tables the database does not have.
func InspectWispChildRows(ctx context.Context, db *sql.DB, database string) ([]WispChildTableState, error) {
	conn, err := useDatabase(ctx, db, database)
	if err != nil {
		return nil, err
	}
	defer conn.Close() //nolint:errcheck // read-only connection

	present, err := tablesIn(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("listing tables in %q: %w", database, err)
	}
	if !present["wisps"] {
		return nil, nil
	}
	cascades, err := cascadeForeignKeysIn(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("listing foreign keys in %q: %w", database, err)
	}
	var states []WispChildTableState
	for _, t := range WispChildTables {
		if !present[t.Table] {
			continue
		}
		var orphans int
		//nolint:gosec // G201: table and column names come from WispChildTables
		query := fmt.Sprintf("SELECT COUNT(*) FROM `%s` c LEFT JOIN wisps w ON w.id = c.`%s` WHERE w.id IS NULL", t.Table, t.Column)
		if err := conn.QueryRowContext(ctx, query).Scan(&orphans); err != nil {
			return nil, fmt.Errorf("counting orphaned %s rows in %q: %w", t.Table, database, err)
		}
		states = append(states, WispChildTableState{WispChildTable: t, Orphans: orphans, HasCascade: cascades[t.FK]})
	}
	return states, nil
}

// RepairWispChildRows deletes database's orphaned wisp child rows in batches of
// at most batchSize parent ids, then restores each missing cascade foreign key
// so later wisp deletes remove their child rows in the same transaction. It
// returns the number of rows deleted per table.
func RepairWispChildRows(ctx context.Context, db *sql.DB, database string, batchSize int) (map[string]int, error) {
	if batchSize <= 0 {
		return nil, fmt.Errorf("repairing wisp child rows in %q: batch size %d must be positive", database, batchSize)
	}
	states, err := InspectWispChildRows(ctx, db, database)
	if err != nil {
		return nil, err
	}
	conn, err := useDatabase(ctx, db, database)
	if err != nil {
		return nil, err
	}
	defer conn.Close() //nolint:errcheck // nothing to flush

	deleted := make(map[string]int)
	for _, s := range states {
		n, err := deleteOrphanedChildRows(ctx, conn, s.WispChildTable, batchSize)
		deleted[s.Table] = n
		if err != nil {
			return deleted, fmt.Errorf("deleting orphaned %s rows in %q: %w", s.Table, database, err)
		}
		if s.HasCascade {
			continue
		}
		//nolint:gosec // G201: identifiers come from WispChildTables
		ddl := fmt.Sprintf("ALTER TABLE `%s` ADD CONSTRAINT `%s` FOREIGN KEY (`%s`) REFERENCES wisps(id) ON DELETE CASCADE ON UPDATE CASCADE", s.Table, s.FK, s.Column)
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			return deleted, fmt.Errorf("adding %s to %s in %q: %w", s.FK, s.Table, database, err)
		}
	}
	return deleted, nil
}

func deleteOrphanedChildRows(ctx context.Context, conn *sql.Conn, t WispChildTable, batchSize int) (int, error) {
	//nolint:gosec // G201: table and column names come from WispChildTables
	selectBatch := fmt.Sprintf("SELECT DISTINCT c.`%s` FROM `%s` c LEFT JOIN wisps w ON w.id = c.`%s` WHERE w.id IS NULL LIMIT ?", t.Column, t.Table, t.Column)
	total := 0
	for {
		ids, err := queryStrings(ctx, conn, selectBatch, batchSize)
		if err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		//nolint:gosec // G201: identifiers come from WispChildTables; values are bound
		res, err := conn.ExecContext(ctx, fmt.Sprintf("DELETE FROM `%s` WHERE `%s` IN (%s)", t.Table, t.Column, placeholders), args...)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += int(n)
	}
}

func useDatabase(ctx context.Context, db *sql.DB, database string) (*sql.Conn, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening connection for %q: %w", database, err)
	}
	//nolint:gosec // G201: identifier-escaped
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("USE `%s`", strings.ReplaceAll(database, "`", "``"))); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("selecting database %q: %w", database, err)
	}
	return conn, nil
}

func tablesIn(ctx context.Context, conn *sql.Conn) (map[string]bool, error) {
	names, err := queryStrings(ctx, conn, "SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = DATABASE()")
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(names))
	for _, name := range names {
		present[strings.ToLower(name)] = true
	}
	return present, nil
}

func cascadeForeignKeysIn(ctx context.Context, conn *sql.Conn) (map[string]bool, error) {
	names, err := queryStrings(ctx, conn, "SELECT CONSTRAINT_NAME FROM INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS WHERE CONSTRAINT_SCHEMA = DATABASE() AND REFERENCED_TABLE_NAME = 'wisps' AND DELETE_RULE = 'CASCADE'")
	if err != nil {
		return nil, err
	}
	cascades := make(map[string]bool, len(names))
	for _, name := range names {
		cascades[name] = true
	}
	return cascades, nil
}

func queryStrings(ctx context.Context, conn *sql.Conn, query string, args ...any) ([]string, error) {
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // read-only cursor
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
