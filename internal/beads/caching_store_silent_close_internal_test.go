package beads

import (
	"context"
	"encoding/json"
	"testing"
)

// silentCloseFixture primes a cache over one open bead, then closes that bead
// behind the cache's back (another process's bd close), which is how a
// workflow root is closed by the control dispatcher.
func silentCloseFixture(t *testing.T) (*CachingStore, *MemStore, string, *[]string) {
	t.Helper()
	mem := NewMemStore()
	root, err := mem.Create(Bead{Title: "workflow root"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	var notices []string
	cache := NewCachingStoreForTest(mem, func(eventType, beadID string, _ json.RawMessage) {
		notices = append(notices, eventType+":"+beadID)
	})
	if err := cache.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}
	if err := mem.Close(root.ID); err != nil {
		t.Fatalf("external Close: %v", err)
	}
	return cache, mem, root.ID, &notices
}

func countNotices(notices []string, want string) int {
	n := 0
	for _, got := range notices {
		if got == want {
			n++
		}
	}
	return n
}

// A live list that returns a bead now closed installs the closed row without
// announcing it. The reconcile then finds the row missing from its open-only
// scan, and must announce the close rather than assume it already went out
// (gsc-bilu: gcd-q4s2g4's root was evicted in a pass that logged removes=2 and
// emitted no bead.closed).
func TestReconcileAnnouncesCloseAbsorbedByALiveListRead(t *testing.T) {
	cache, _, id, notices := silentCloseFixture(t)

	if _, err := cache.List(ListQuery{Live: true, AllowScan: true, IncludeClosed: true}); err != nil {
		t.Fatalf("live List: %v", err)
	}
	if got, err := cache.Get(id); err != nil || got.Status != "closed" {
		t.Fatalf("cached row after live read = %+v, %v; want closed", got, err)
	}

	cache.runReconciliation()
	if n := countNotices(*notices, "bead.closed:"+id); n != 1 {
		t.Fatalf("bead.closed notices for %s = %d, want 1 (all: %v)", id, n, *notices)
	}
	cache.runReconciliation()
	if n := countNotices(*notices, "bead.closed:"+id); n != 1 {
		t.Fatalf("a second pass re-announced the close: %d notices (all: %v)", n, *notices)
	}
}

// A Get that reads a dirty row through to the backing installs the closed row
// the same silent way.
func TestReconcileAnnouncesCloseAbsorbedByADirtyGet(t *testing.T) {
	cache, _, id, notices := silentCloseFixture(t)

	cache.mu.Lock()
	cache.markDirtyLocked(id)
	cache.mu.Unlock()
	if got, err := cache.Get(id); err != nil || got.Status != "closed" {
		t.Fatalf("dirty Get = %+v, %v; want the closed backing row", got, err)
	}

	cache.runReconciliation()
	if n := countNotices(*notices, "bead.closed:"+id); n != 1 {
		t.Fatalf("bead.closed notices for %s = %d, want 1 (all: %v)", id, n, *notices)
	}
}

// A close the cache learned from the event bus was announced by whoever wrote
// that event. Re-announcing it on eviction would feed the event back into the
// bus it came from.
func TestReconcileDoesNotReannounceCloseAppliedFromTheEventBus(t *testing.T) {
	cache, mem, id, notices := silentCloseFixture(t)

	closed, err := mem.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	payload, err := EncodeBeadEventPayload(closed)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	cache.ApplyEvent("bead.closed", payload)

	cache.runReconciliation()
	if n := countNotices(*notices, "bead.closed:"+id); n != 0 {
		t.Fatalf("bus-applied close re-announced %d time(s) (all: %v)", n, *notices)
	}
}

// A close the cache made itself is announced once, by the write.
func TestReconcileDoesNotReannounceALocalClose(t *testing.T) {
	mem := NewMemStore()
	root, err := mem.Create(Bead{Title: "workflow root"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	var notices []string
	cache := NewCachingStoreForTest(mem, func(eventType, beadID string, _ json.RawMessage) {
		notices = append(notices, eventType+":"+beadID)
	})
	if err := cache.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}
	if err := cache.Close(root.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	cache.runReconciliation()
	if n := countNotices(notices, "bead.closed:"+root.ID); n != 1 {
		t.Fatalf("bead.closed notices = %d, want exactly the write's 1 (all: %v)", n, notices)
	}
}
