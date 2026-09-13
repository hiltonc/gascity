package api

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
)

// gatedStore counts the bead lookups an agent-list build makes and, once
// armed, holds the next one open until release is closed. A test can then put
// several requests in flight against one unfinished build: any request that
// does NOT join that build reaches the store and shows up in the count.
type gatedStore struct {
	beads.Store

	mu      sync.Mutex
	calls   int
	armed   bool
	entered chan struct{}
	release chan struct{}
}

func newGatedStore() *gatedStore {
	return &gatedStore{
		Store:   beads.NewMemStore(),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

// arm makes the next lookup block until release is closed.
func (s *gatedStore) arm() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.armed = true
}

func (s *gatedStore) gate() {
	s.mu.Lock()
	s.calls++
	hold := s.armed
	if hold {
		// Only the first lookup after arming blocks; the rest of that same
		// build runs normally once released.
		s.armed = false
		close(s.entered)
	}
	s.mu.Unlock()
	if hold {
		<-s.release
	}
}

func (s *gatedStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *gatedStore) resetCount() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = 0
}

// Both list methods share the gate because buildAgentList may reach the store
// through either.
func (s *gatedStore) List(query beads.ListQuery) ([]beads.Bead, error) {
	s.gate()
	return s.Store.List(query)
}

func (s *gatedStore) ListByAssignee(assignee, status string, limit int) ([]beads.Bead, error) {
	s.gate()
	return s.Store.ListByAssignee(assignee, status, limit)
}

// pinBuckets pins the two knobs cachedAgentList reads so a test can separate
// its two lookups. The bucket is held wide enough that it never rolls on its
// own; a test that needs a rolled bucket or an expired floor puts the entry
// in that state with rollCachedResponseBucket and ageCachedResponse rather
// than waiting for wall-clock time to pass.
func pinBuckets(t *testing.T, floor time.Duration) {
	t.Helper()
	prevTTL := timeBucketResponseCacheTTL
	prevFloor := agentListResponseTTLFloor
	timeBucketResponseCacheTTL = time.Hour
	agentListResponseTTLFloor = floor
	t.Cleanup(func() {
		timeBucketResponseCacheTTL = prevTTL
		agentListResponseTTLFloor = prevFloor
	})
}

// agentListCacheKey is the key humaHandleAgentList derives for an unfiltered
// list, which is the entry the tests below reach into.
func agentListCacheKey() string {
	return cacheKeyFor("agents", &AgentListInput{})
}

// rollCachedResponseBucket moves the stored entry out of the wall-clock
// bucket it was written in, leaving its age untouched. That is the state
// every build reaches on a loaded host — it outlives its own bucket —
// reproduced as state rather than as elapsed time.
func rollCachedResponseBucket(t *testing.T, s *Server, key string) {
	t.Helper()
	s.responseCacheMu.Lock()
	defer s.responseCacheMu.Unlock()
	entry, ok := s.responseCacheEntries[key]
	if !ok {
		t.Fatalf("no cache entry for %q to roll: the build stored nothing", key)
	}
	entry.index--
	s.responseCacheEntries[key] = entry
}

// ageCachedResponse backdates the stored entry past maxAge, so the age-floor
// lookup — and the index-keyed TTL measured from the same timestamp — both
// treat it as too old to serve.
func ageCachedResponse(t *testing.T, s *Server, key string, maxAge time.Duration) {
	t.Helper()
	s.responseCacheMu.Lock()
	defer s.responseCacheMu.Unlock()
	entry, ok := s.responseCacheEntries[key]
	if !ok {
		t.Fatalf("no cache entry for %q to age: the build stored nothing", key)
	}
	entry.storedAt = entry.storedAt.Add(-(maxAge + time.Minute))
	s.responseCacheEntries[key] = entry
}

func listAgents(t *testing.T, srv *Server) *ListOutput[agentResponse] {
	t.Helper()
	out, err := srv.humaHandleAgentList(context.Background(), &AgentListInput{})
	if err != nil {
		t.Fatalf("agent list: %v", err)
	}
	return out
}

// The entry is stored under the bucket the build FINISHED in, and read back
// through an age floor as well as an exact-bucket lookup. Without both, an
// entry whose build outlived its bucket is unreadable — which is every build
// on the loaded hosts this cache exists for, so the cache never hit.
func TestAgentListServesARecentBodyAfterTheBucketRolls(t *testing.T) {
	pinBuckets(t, time.Hour)

	state := newFakeState(t)
	store := &countingStore{Store: beads.NewMemStore()}
	state.stores["myrig"] = store
	srv := New(state)

	listAgents(t, srv)
	built := store.listByAssigneeCalls
	if built == 0 {
		t.Fatalf("first list made no bead lookups; the fixture is not exercising the build")
	}

	// The entry is no longer in the current bucket, so the exact-bucket
	// lookup cannot match and a hit here can only come from the age floor.
	rollCachedResponseBucket(t, srv, agentListCacheKey())

	listAgents(t, srv)
	if store.listByAssigneeCalls != built {
		t.Errorf("lookups after the bucket rolled = %d, want %d: the body built a moment ago was not reused", store.listByAssigneeCalls, built)
	}
}

// The floor is a floor, not a lease: once a body is older than it, and its
// bucket has rolled, the next request rebuilds.
func TestAgentListRebuildsOnceTheTTLFloorExpires(t *testing.T) {
	pinBuckets(t, time.Millisecond)

	state := newFakeState(t)
	store := &countingStore{Store: beads.NewMemStore()}
	state.stores["myrig"] = store
	srv := New(state)

	listAgents(t, srv)
	built := store.listByAssigneeCalls
	if built == 0 {
		t.Fatalf("first list made no bead lookups; the fixture is not exercising the build")
	}

	key := agentListCacheKey()
	rollCachedResponseBucket(t, srv, key)
	ageCachedResponse(t, srv, key, agentListResponseTTLFloor)

	listAgents(t, srv)
	if store.listByAssigneeCalls <= built {
		t.Errorf("lookups after the floor expired = %d, want more than %d: a stale body was served past its TTL", store.listByAssigneeCalls, built)
	}
}

// Requests that coalesce onto one build are each handed the leader's value,
// so each needs its own copy: a body several goroutines hold must not be one
// a later partial-error note can append to in place. The sibling cache path
// deep-copies for the same reason (cloneCachedValue).
func TestAgentListCoalescedRequestsGetIndependentBodies(t *testing.T) {
	const callers = 4

	// A short floor to start with; the warm-up's entry is dropped outright
	// below, so the burst has to reach the build either way.
	pinBuckets(t, time.Millisecond)

	state := newFakeState(t)
	store := newGatedStore()
	state.stores["myrig"] = store
	srv := New(state)

	// One sequential warm-up to learn how many lookups a single build makes;
	// the burst below must add exactly that many, no matter how many callers.
	listAgents(t, srv)
	perBuild := store.callCount()
	if perBuild == 0 {
		t.Fatalf("the warm-up list made no bead lookups; the fixture is not exercising the build")
	}
	// Drop the warm-up's entry so the leader below has to build, and raise
	// the floor so the leader's own entry stays readable for the rest of the
	// test. A follower that arrives after the leader's build has finished is
	// then served that entry — itself a clone, via cachedResponseWithinAgeAs
	// — instead of missing and starting a second build. Neither assertion
	// below then depends on which side of the leader's completion a follower
	// lands on. pinBuckets restores the floor.
	srv.responseCacheMu.Lock()
	srv.responseCacheEntries = nil
	srv.responseCacheMu.Unlock()
	agentListResponseTTLFloor = time.Hour

	store.resetCount()
	store.arm()

	outs := make(chan *ListOutput[agentResponse], callers)
	errs := make(chan error, callers)
	// Each caller announces itself immediately before the handler call, so
	// the test can prove every goroutine is running and at the call site
	// rather than sleeping long enough that it probably is.
	atCallSite := make(chan struct{}, callers)
	call := func() {
		atCallSite <- struct{}{}
		out, err := srv.humaHandleAgentList(context.Background(), &AgentListInput{})
		if err != nil {
			errs <- err
			outs <- nil
			return
		}
		outs <- out
	}

	// One leader, held open inside its build, then the rest pile onto the
	// same cache key. Nothing is cached until the leader finishes, so a
	// follower that does not join the leader's build must reach the store —
	// which the lookup count below would catch.
	go call()
	<-atCallSite
	select {
	case <-store.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("the first list never reached the bead store; the fixture is not exercising the build")
	}
	for i := 1; i < callers; i++ {
		go call()
	}
	// A barrier, not a sleep: release the leader only once every follower
	// goroutine is running and at the handler call. A follower that still
	// arrives after the leader finishes is served the leader's cache entry,
	// which makes no store call and is its own clone, so both assertions
	// below hold either way.
	for i := 1; i < callers; i++ {
		select {
		case <-atCallSite:
		case <-time.After(30 * time.Second):
			t.Fatal("a follower goroutine never reached the agent-list call")
		}
	}
	close(store.release)

	seen := make([]*ListOutput[agentResponse], 0, callers)
	for i := 0; i < callers; i++ {
		select {
		case out := <-outs:
			if out == nil {
				t.Fatalf("agent list returned an error: %v", <-errs)
			}
			seen = append(seen, out)
		case <-time.After(30 * time.Second):
			t.Fatal("an agent list request never returned")
		}
	}

	if n := store.callCount(); n != perBuild {
		t.Errorf("bead lookups across %d concurrent requests = %d, want %d (one build): the requests did not share the leader's build", callers, n, perBuild)
	}

	backing := map[*agentResponse]int{}
	for _, out := range seen {
		if len(out.Body.Items) == 0 {
			t.Fatal("agent list returned no items; the fixture declares one agent")
		}
		backing[&out.Body.Items[0]]++
	}
	if len(backing) != callers {
		t.Errorf("distinct item arrays = %d, want %d: callers share one body, so a mutation by any of them is visible to the rest", len(backing), callers)
	}
}
