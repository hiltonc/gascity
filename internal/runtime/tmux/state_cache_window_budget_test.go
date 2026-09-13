package tmux

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// blockingWindowExecutor answers list-panes at once and blocks list-windows
// until its own context is canceled, so a test can observe exactly how much
// of the fetch budget the window listing is allowed to spend.
type blockingWindowExecutor struct {
	panes string
}

func (e *blockingWindowExecutor) execute(args []string) (string, error) {
	return e.executeCtx(context.Background(), args)
}

func (e *blockingWindowExecutor) executeCtx(ctx context.Context, args []string) (string, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "list-panes"):
		return e.panes, nil
	case strings.Contains(joined, "list-windows"):
		<-ctx.Done()
		return "", ctx.Err()
	}
	return "", nil
}

// The window listing is the second of three subprocesses FetchState runs in
// series under one fetchTimeout. The third, fetchProcessSnapshot, feeds
// processAlive, which the reconciler's liveness refinement reads — so a
// listing that could spend the whole budget would degrade a reconciler input
// on exactly the loaded hosts this optimization targets. Its own sub-budget
// is what makes that impossible; this asserts the parent budget it leaves
// behind, which is the part the process snapshot depends on.
func TestListWindowsCannotConsumeTheWholeFetchBudget(t *testing.T) {
	tm := NewTmux()
	tm.exec = &blockingWindowExecutor{}
	f := &tmuxFetcher{tm: tm}

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	start := time.Now()
	_, err := f.listWindows(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("listWindows returned no error; the stub never answers, so it must hit its own bound")
	}
	if ctx.Err() != nil {
		t.Fatalf("parent context is done after %v: the listing consumed the whole fetch budget", elapsed)
	}
	if elapsed >= fetchTimeout {
		t.Errorf("listWindows took %v, want under the %v fetch budget", elapsed, fetchTimeout)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("parent context has no deadline")
	}
	// What the process snapshot after it still has to work with.
	left := time.Until(deadline)
	if want := fetchTimeout - windowListingBudget; left < want-500*time.Millisecond {
		t.Errorf("%v of the fetch budget left after the window listing, want about %v", left, want)
	}
}

// The sub-budget is a cap on the listing, not a floor on it: a listing that
// answers at once must not be made to wait, and its output must still fold
// onto the sessions the panes walk found.
func TestListWindowsFoldsAListingThatAnswersAtOnce(t *testing.T) {
	tm := NewTmux()
	tm.exec = &fakeExecutor{outs: []string{"gc-a\t1\t1700000900\n"}}
	f := &tmuxFetcher{tm: tm}

	start := time.Now()
	windows, err := f.listWindows(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("listWindows: %v", err)
	}
	if elapsed >= windowListingBudget {
		t.Errorf("listWindows took %v; a listing that answered at once must not wait out the %v sub-budget", elapsed, windowListingBudget)
	}

	sessions := map[string]sessionRuntimeState{"gc-a": {Running: true}}
	applyWindowListing(sessions, windows)
	if !sessions["gc-a"].Attached {
		t.Errorf("gc-a attached = false, want true from the window listing")
	}
}

// windowListingBudget bounds the listing; it does not reserve anything for
// the process snapshot that runs after it. The panes walk ahead of the
// listing has no sub-budget of its own, so on a loaded host it can spend the
// fetch budget down to a sliver — and the listing would then run inside the
// tail fetchProcessSnapshot needs, starving the reconciler's liveness input
// on exactly the hosts this optimization targets. This pins the reserve: a
// listing that starts late must end early enough to leave it.
func TestListWindowsLeavesTheProcessSnapshotItsReserve(t *testing.T) {
	tm := NewTmux()
	tm.exec = &blockingWindowExecutor{}
	f := &tmuxFetcher{tm: tm}

	// What the parent context looks like after a slow panes walk: less of the
	// fetch budget remains than the listing's own cap would spend.
	remaining := processSnapshotReserve + 200*time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), remaining)
	defer cancel()

	start := time.Now()
	if _, err := f.listWindows(ctx); err == nil {
		t.Fatal("listWindows returned no error; the stub never answers, so it must hit a bound")
	}
	elapsed := time.Since(start)

	if ctx.Err() != nil {
		t.Fatalf("parent context is done after %v: the listing consumed the process snapshot's reserve", elapsed)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("parent context has no deadline")
	}
	if left := time.Until(deadline); left < processSnapshotReserve-100*time.Millisecond {
		t.Errorf("%v of the fetch budget left after the window listing, want at least the %v process-snapshot reserve", left, processSnapshotReserve)
	}
}

// Once the panes walk has spent past the reserve there is no budget left to
// lend: running the listing at all would come out of fetchProcessSnapshot's
// share, so it is skipped and attach state degrades to per-session probes.
func TestListWindowsIsSkippedWhenOnlyTheReserveRemains(t *testing.T) {
	exec := &blockingWindowExecutor{}
	tm := NewTmux()
	tm.exec = exec
	f := &tmuxFetcher{tm: tm}

	ctx, cancel := context.WithTimeout(context.Background(), processSnapshotReserve/2)
	defer cancel()

	start := time.Now()
	_, err := f.listWindows(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, errWindowListingBudgetExhausted) {
		t.Fatalf("listWindows error = %v, want errWindowListingBudgetExhausted", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("listWindows took %v before declining; a skip must not spend budget", elapsed)
	}
	if ctx.Err() != nil {
		t.Errorf("parent context is done: the skip still spent the remaining budget")
	}
}
