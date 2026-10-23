package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/doctor"
	"github.com/gastownhall/gascity/internal/doltauth"
)

// wispChildRowsRepairBatch bounds each orphan DELETE so a backlog of millions
// of rows never holds one long write transaction on a shared server.
const wispChildRowsRepairBatch = 500

// wispChildRowsCheck reports, per database on the city's Dolt server, wisp
// child rows whose wisp is gone and child tables missing the cascade foreign
// key that should have deleted them. Every bead query aggregates wisp_labels,
// so orphans there slow the whole store.
type wispChildRowsCheck struct {
	openDB func() (*sql.DB, error)
}

func newWispChildRowsCheck(cityPath string, cfg *config.City) *wispChildRowsCheck {
	return &wispChildRowsCheck{openDB: func() (*sql.DB, error) { return openCityDoltDB(cityPath, cfg) }}
}

func openCityDoltDB(cityPath string, cfg *config.City) (*sql.DB, error) {
	host := strings.TrimSpace(cfg.Dolt.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	resolution := ResolveDoltPort(PortResolverInput{
		CityPort: cfg.Dolt.Port,
		CityPath: cityPath,
	})
	if err := fatalPortResolutionError(resolution); err != nil {
		return nil, fmt.Errorf("resolving dolt port: %w", err)
	}
	if resolution.Fallback {
		return nil, fmt.Errorf("no live managed dolt endpoint for %s (resolution fell back to legacy port %d)", cityPath, resolution.Port)
	}
	user := doltauth.Resolve(cityPath, "root", host, resolution.Port).User
	return managedDoltOpenDB(host, strconv.Itoa(resolution.Port), user)
}

// Name implements doctor.Check.
func (c *wispChildRowsCheck) Name() string { return "wisp-child-rows" }

// CanFix implements doctor.Check.
func (c *wispChildRowsCheck) CanFix() bool { return true }

// WarmupEligible implements doctor.Check.
func (c *wispChildRowsCheck) WarmupEligible() bool { return false }

// Run implements doctor.Check.
func (c *wispChildRowsCheck) Run(_ *doctor.CheckContext) *doctor.CheckResult {
	res := &doctor.CheckResult{Name: c.Name()}
	unhealthy, err := c.inspect(context.Background())
	if err != nil {
		res.Status = doctor.StatusWarning
		res.Message = fmt.Sprintf("wisp child rows unknown: %v", err)
		return res
	}
	if len(unhealthy) == 0 {
		res.Status = doctor.StatusOK
		res.Message = "no orphaned wisp child rows; wisp deletes cascade"
		return res
	}
	res.Status = doctor.StatusWarning
	orphans, uncascaded := 0, 0
	for _, db := range sortedMapKeys(unhealthy) {
		for _, s := range unhealthy[db] {
			orphans += s.Orphans
			detail := fmt.Sprintf("%s.%s: %d orphaned row(s)", db, s.Table, s.Orphans)
			if !s.HasCascade {
				uncascaded++
				detail += fmt.Sprintf(", missing ON DELETE CASCADE foreign key %s", s.FK)
			}
			res.Details = append(res.Details, detail)
		}
	}
	res.Message = fmt.Sprintf("%d orphaned wisp child row(s) and %d table(s) without a cascade foreign key across %d database(s)", orphans, uncascaded, len(unhealthy))
	res.FixHint = "run gc doctor --fix to delete the orphans in batches and restore the cascade foreign keys"
	return res
}

// Fix implements doctor.Check.
func (c *wispChildRowsCheck) Fix(_ *doctor.CheckContext) error {
	ctx := context.Background()
	db, names, err := c.userDatabases(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range names {
		if _, err := beads.RepairWispChildRows(ctx, db, name, wispChildRowsRepairBatch); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *wispChildRowsCheck) inspect(ctx context.Context) (map[string][]beads.WispChildTableState, error) {
	db, names, err := c.userDatabases(ctx)
	if err != nil {
		return nil, err
	}
	unhealthy := make(map[string][]beads.WispChildTableState)
	for _, name := range names {
		states, err := beads.InspectWispChildRows(ctx, db, name)
		if err != nil {
			return nil, err
		}
		for _, s := range states {
			if !s.Healthy() {
				unhealthy[name] = append(unhealthy[name], s)
			}
		}
	}
	return unhealthy, nil
}

func (c *wispChildRowsCheck) userDatabases(ctx context.Context) (*sql.DB, []string, error) {
	db, err := c.openDB()
	if err != nil {
		return nil, nil, fmt.Errorf("opening dolt connection: %w", err)
	}
	all, err := (&sqlCleanupDoltClient{db: db}).ListDatabases(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("listing databases: %w", err)
	}
	var names []string
	for _, name := range all {
		if _, system := managedDoltSystemDatabases[strings.ToLower(name)]; !system {
			names = append(names, name)
		}
	}
	return db, names, nil
}
