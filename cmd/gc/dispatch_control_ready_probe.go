package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/beads/contract"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/doltauth"
	"github.com/gastownhall/gascity/internal/doltpool"
	"github.com/gastownhall/gascity/internal/fsys"
)

// This file lets an idle --follow sweep skip re-listing a rig whose ledger has
// not changed (gsc-dtay).
//
// Without it, every sweep past controlReadyCacheTTL re-primed the scope's
// snapshot. The follow loop's idle sleep (workflowServeMaxIdleSleep, 5s) is
// longer than that TTL (3s), so the snapshot was never reused across sweeps and
// a quiet rig paid a full re-list every sweep. A re-prime is one brief `bd
// list` plus the ready projection's `bd sql` (CachingStore.PrimeActiveReadiness,
// controlBdVersionMemo).
//
// The sweep cannot simply stop polling. Workers close step beads with plain bd
// writes that publish no city event, so the timer sweep is what notices a newly
// ready control bead; on the Dispatch host 1394 of 1748 processing drains
// started from a timer sweep, not an event. Waiting for the supervisor's
// reconcile to surface those writes as events would stretch that pickup from
// ~5s to its 30-120s cadence.
//
// So the sweep keeps its cadence and asks a cheaper question first: has the
// ledger changed since the snapshot was taken? dolt_hashof_db() is Dolt's
// content hash of the whole database including the uncommitted working set, so
// any write moves it and a re-read does not. It is one query on a pooled
// in-process connection -- no subprocess -- and only a moved hash, a failed
// probe, or controlReadyCacheMaxAge sends the sweep to a re-prime.

// controlReadyCacheMaxAge bounds how long a snapshot may be reused on the
// strength of an unchanged change token. The probe should see every write that
// can change readiness, and CachedReady evaluates defer_until against the clock
// at read time, so this is a backstop against a change the token cannot see
// rather than part of the readiness contract. It matches the supervisor bead
// cache's slowest reconcile cadence, so the follower never re-lists a quiet rig
// more often than the supervisor's own cache would.
const controlReadyCacheMaxAge = 2 * time.Minute

// controlReadyChangeProbeTimeout bounds one probe query. A probe that cannot
// answer in time falls back to a re-prime, which is the pre-probe behavior.
const controlReadyChangeProbeTimeout = 2 * time.Second

// errControlReadyProbeUnsupported marks a scope whose store is not a bd ledger
// on a Dolt server, so it has no database hash to read.
var errControlReadyProbeUnsupported = errors.New("scope is not served by a Dolt sql-server")

// controlReadyChangeTokenFn is the test seam for controlReadyChangeToken.
var controlReadyChangeTokenFn = controlReadyChangeToken

// controlReadyChangeToken returns a token that changes whenever the ledger
// serving dir's own store is written: the Dolt database hash, read over the
// process-shared doltpool connection for the scope's endpoint. Any error means
// "unknown", never "unchanged".
func controlReadyChangeToken(dir, cityPath string) (string, error) {
	scopeRoot := resolveStoreScopeRoot(cityPath, dir)
	provider := rawBeadsProviderForScope(scopeRoot, cityPath)
	if !providerUsesBdStoreContract(provider) || cityUsesDoltliteBeadsBackend(cityPath) {
		return "", fmt.Errorf("change probe for %s: provider %q: %w", scopeRoot, provider, errControlReadyProbeUnsupported)
	}
	target, err := contract.ResolveDoltConnectionTarget(fsys.OSFS{}, cityPath, scopeRoot)
	if err != nil {
		return "", fmt.Errorf("change probe for %s: resolving dolt endpoint: %w", scopeRoot, err)
	}
	host := strings.TrimSpace(target.Host)
	portText := strings.TrimSpace(target.Port)
	port, err := strconv.Atoi(portText)
	if err != nil || host == "" {
		return "", fmt.Errorf("change probe for %s: incomplete dolt endpoint %q:%q", scopeRoot, host, portText)
	}
	auth := doltauth.Resolve(doltauth.AuthScopeRoot(cityPath, scopeRoot, target), strings.TrimSpace(target.User), host, port)
	user := strings.TrimSpace(auth.User)
	if user == "" {
		// The same default bd and the other Go-native Dolt readers apply
		// (managedDoltOpenDatabase, the API's buildDoltDSN).
		user = "root"
	}
	db, err := doltpool.Open(host, portText, user, auth.Password, target.Database)
	if err != nil {
		return "", fmt.Errorf("change probe for %s: %w", scopeRoot, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlReadyChangeProbeTimeout)
	defer cancel()
	var token string
	if err := db.QueryRowContext(ctx, "SELECT dolt_hashof_db()").Scan(&token); err != nil {
		return "", fmt.Errorf("change probe for %s: reading database hash: %w", scopeRoot, err)
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("change probe for %s: empty database hash", scopeRoot)
	}
	return token, nil
}

// controlBdVersionMemo shares one `bd version` answer across the control
// stores this process opens. The control-ready scan builds a fresh scope store
// on every re-prime, and without the memo each one re-spawned `bd version` for
// its ready-projection and --brief gates.
var controlBdVersionMemo = beads.NewBdVersionMemo()

// controlBdStoreOptions is bdStoreOptionsForConfig plus the shared version memo.
func controlBdStoreOptions(cfg *config.City) []beads.BdStoreOption {
	return append(bdStoreOptionsForConfig(cfg), beads.WithBdStoreVersionMemo(controlBdVersionMemo))
}

// controlReadyProbeWarned keeps the "probe unavailable" notice to one line per
// scope per process: the sweep asks every few seconds and the answer is usually
// a standing property of the scope.
var controlReadyProbeWarned sync.Map

// probeControlReadyChangeToken runs the probe and reports its failure once per
// scope, so an operator can see the follower has fallen back to re-listing on
// every sweep.
func probeControlReadyChangeToken(dir, cityPath string) (string, bool) {
	token, err := controlReadyChangeTokenFn(dir, cityPath)
	if err != nil {
		if _, seen := controlReadyProbeWarned.LoadOrStore(dir, struct{}{}); !seen {
			log.Printf("control-ready cache: change probe unavailable for %s, re-priming every %s instead: %v", dir, controlReadyCacheTTL, err)
		}
		return "", false
	}
	return token, true
}

// controlReadyEntryReusable reports whether entry may answer another sweep:
// inside the TTL unconditionally (the pre-probe rule), and past it only while
// the scope's change token still matches the one taken before the prime.
//
// observed is the token the check read, or "" when it read none. A moved token
// is returned so the re-prime can adopt it as its own pre-prime token rather
// than probing a second time: it was read before that prime, which is all the
// ordering rule on controlReadyCacheEntry.changeToken asks.
func controlReadyEntryReusable(entry *controlReadyCacheEntry, dir, cityPath string) (reusable bool, observed string) {
	age := time.Since(entry.primedAt)
	if age < controlReadyCacheTTL {
		return true, ""
	}
	if entry.changeToken == "" || age >= controlReadyCacheMaxAge {
		return false, ""
	}
	token, ok := probeControlReadyChangeToken(dir, cityPath)
	return ok && token == entry.changeToken, token
}
