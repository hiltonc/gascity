package events

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingReaderAt wraps an io.ReaderAt and totals the bytes handed to ReadAt,
// so a test can assert that a bounded tail walk stopped early instead of
// walking the whole file.
type countingReaderAt struct {
	f    *os.File
	read atomic.Int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.f.ReadAt(p, off)
	c.read.Add(int64(n))
	return n, err
}

// writeTimedLog writes n events, seq 1..n, one second apart ending at base.
// Every third event is type "rare" so a selective filter has something to
// discriminate on that the recent window does not satisfy in bulk.
func writeTimedLog(t *testing.T, n int, base time.Time) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	var sb strings.Builder
	for i := 0; i < n; i++ {
		seq := uint64(i + 1)
		typ := "common"
		if seq%3 == 0 {
			typ = "rare"
		}
		ts := base.Add(-time.Duration(n-1-i) * time.Second).UTC().Format(time.RFC3339Nano)
		// Pad the payload so the log spans many 64 KiB chunks and a short
		// walk is distinguishable from a full one by bytes read.
		fmt.Fprintf(&sb, `{"seq":%d,"type":%q,"ts":%q,"actor":"t","subject":"s%d","message":%q}`+"\n",
			seq, typ, ts, seq, strings.Repeat("x", 512))
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	return path
}

// tailFile walks path backwards through readFilteredTailFrom and reports both
// the matching events and how many bytes the walk actually read, so a test can
// tell an early stop from a full traversal.
func tailFile(t *testing.T, path string, filter Filter, limit int) ([]Event, int64) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close() //nolint:errcheck // read-only file
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	counter := &countingReaderAt{f: f}
	evts, err := readFilteredTailFrom(counter, info.Size(), filter, limit)
	if err != nil {
		t.Fatalf("readFilteredTailFrom: %v", err)
	}
	return evts, counter.read.Load()
}

// TestTailStopsAtAfterSeqFloor pins the early stop that AfterSeq buys. The log
// is strictly seq-ordered, so the first event at or below the cursor ends the
// backward walk instead of dragging it to the head of the file.
func TestTailStopsAtAfterSeqFloor(t *testing.T) {
	const n = 4000
	path := writeTimedLog(t, n, time.Now().UTC())
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	evts, readBytes := tailFile(t, path, Filter{AfterSeq: n - 10}, 500)

	if got, want := len(evts), 10; got != want {
		t.Fatalf("got %d events, want %d", got, want)
	}
	if readBytes >= info.Size() {
		t.Fatalf("bounded tail read %d bytes of a %d byte log; it must stop at the AfterSeq floor", readBytes, info.Size())
	}
}

// TestTailWithoutFloorWalksTheFile is the contrast case: with no AfterSeq the
// walk has no floor to stop at, so it reaches the head and still filters
// correctly.
func TestTailWithoutFloorWalksTheFile(t *testing.T) {
	path := writeTimedLog(t, 20, time.Now().UTC())

	evts, _ := tailFile(t, path, Filter{Type: "rare"}, 500)

	if len(evts) == 0 {
		t.Fatal("expected the matching events from the active file")
	}
	for i, e := range evts {
		if e.Type != "rare" {
			t.Fatalf("event %d has type %q, want rare", i, e.Type)
		}
	}
}

// TestTailMaxScanBytesBoundsBackwardWalk pins the byte budget. A Type filter
// that never matches near EOF — the common case for a rare or optional event
// type — otherwise forces the backward walk to read the entire file, at the
// same cost as the unfiltered forward scan it was meant to avoid. The budget
// caps that walk, and "no match within the window" is the answer.
func TestTailMaxScanBytesBoundsBackwardWalk(t *testing.T) {
	const n = 4000
	path := writeTimedLog(t, n, time.Now().UTC())
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() <= 64*1024 {
		t.Fatalf("log is %d bytes; the fixture must exceed the 64 KiB budget to prove a bound", info.Size())
	}

	// seq 1 is "common", so this filter's only matches sit well below EOF.
	bounded, readBytes := tailFile(t, path, Filter{Subject: "s1", MaxScanBytes: 64 * 1024}, 1)

	if len(bounded) != 0 {
		t.Fatalf("bounded walk got %d events, want 0 (the match sits outside the budget)", len(bounded))
	}
	if readBytes > 2*64*1024 {
		t.Fatalf("bounded walk read %d bytes for a 64 KiB budget", readBytes)
	}

	// The contrast case proves the budget, not the filter, produced the empty
	// result: the same query unbounded finds the match.
	unbounded, _ := tailFile(t, path, Filter{Subject: "s1"}, 1)
	if len(unbounded) != 1 || unbounded[0].Seq != 1 {
		t.Fatalf("unbounded walk got %+v, want the seq-1 match", unbounded)
	}
}

// TestTailMaxScanBytesStopsMidChunk pins the clamp: a budget that is not a
// multiple of the 64 KiB chunk size must stop the walk where the budget ends,
// not up to a full chunk past it.
func TestTailMaxScanBytesStopsMidChunk(t *testing.T) {
	const budget = 100 * 1024 // 1.5 chunks
	path := writeTimedLog(t, 4000, time.Now().UTC())

	_, readBytes := tailFile(t, path, Filter{Subject: "s1", MaxScanBytes: budget}, 1)

	if readBytes > budget {
		t.Fatalf("walk read %d bytes for a %d byte budget; the last chunk must be clamped", readBytes, budget)
	}
}

// TestTailMaxScanBytesDoesNotTruncateAnAnswerableRead pins that the budget
// never changes an answer it can reach: a page the walk fills inside the
// budget is byte-identical to the unbounded one.
func TestTailMaxScanBytesDoesNotTruncateAnAnswerableRead(t *testing.T) {
	path := writeTimedLog(t, 300, time.Now().UTC())

	bounded, _ := tailFile(t, path, Filter{Type: "rare"}, 5)
	unbounded, _ := tailFile(t, path, Filter{Type: "rare", MaxScanBytes: 8 << 20}, 5)

	if len(bounded) != len(unbounded) {
		t.Fatalf("bounded read returned %d events, unbounded %d", len(unbounded), len(bounded))
	}
	for i := range bounded {
		if bounded[i].Seq != unbounded[i].Seq {
			t.Fatalf("event %d: bounded seq %d, unbounded seq %d", i, unbounded[i].Seq, bounded[i].Seq)
		}
	}
}

// TestReadFilteredTailUnchanged pins that the bounded walk did not change the
// events ReadFilteredTail returns for its existing callers.
func TestReadFilteredTailUnchanged(t *testing.T) {
	const n = 300
	base := time.Now().UTC()
	path := writeTimedLog(t, n, base)

	got, err := ReadFilteredTail(path, Filter{Type: "rare"}, 5)
	if err != nil {
		t.Fatalf("ReadFilteredTail: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d events, want 5", len(got))
	}
	for i, e := range got {
		if e.Type != "rare" {
			t.Fatalf("event %d has type %q, want rare", i, e.Type)
		}
		if i > 0 && got[i-1].Seq >= e.Seq {
			t.Fatalf("events are not in ascending seq order: %d then %d", got[i-1].Seq, e.Seq)
		}
	}
	if got, want := got[len(got)-1].Seq, uint64(n); got != want {
		t.Fatalf("newest returned seq = %d, want %d", got, want)
	}
}

// TestTailOnEmptyAndMissingLog pins the post-rotation and cold-start cases: an
// empty or absent active events.jsonl yields no events and no error, leaving
// the caller on its existing archive-aware path.
func TestTailOnEmptyAndMissingLog(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	for name, path := range map[string]string{
		"empty":   empty,
		"missing": filepath.Join(dir, "absent.jsonl"),
	} {
		t.Run(name, func(t *testing.T) {
			evts, err := ReadFilteredTail(path, Filter{AfterSeq: 5}, 100)
			if err != nil {
				t.Fatalf("ReadFilteredTail: %v", err)
			}
			if len(evts) != 0 {
				t.Fatalf("got %d events from a %s log", len(evts), name)
			}
		})
	}
}
