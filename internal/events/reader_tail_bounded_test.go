package events

import (
	"fmt"
	"io"
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

func tailScanFile(t *testing.T, path string, filter Filter, limit int) (TailScan, int64) {
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
	scan, err := readFilteredTailFrom(counter, info.Size(), filter, limit)
	if err != nil {
		t.Fatalf("readFilteredTailFrom: %v", err)
	}
	return scan, counter.read.Load()
}

// TestTailStopsAtAfterSeqFloor is the cursor-shaped equivalent: the log is
// seq-ordered, so the first event at or below AfterSeq ends the walk.
func TestTailStopsAtAfterSeqFloor(t *testing.T) {
	const n = 4000
	path := writeTimedLog(t, n, time.Now().UTC())
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	scan, readBytes := tailScanFile(t, path, Filter{AfterSeq: n - 10}, 500)

	if !scan.Complete {
		t.Fatal("tail that reached the AfterSeq floor must report Complete")
	}
	if got, want := len(scan.Events), 10; got != want {
		t.Fatalf("got %d events, want %d", got, want)
	}
	if readBytes >= info.Size() {
		t.Fatalf("bounded tail read %d bytes of a %d byte log; it must stop at the AfterSeq floor", readBytes, info.Size())
	}
}

// TestTailFullPageIsNotComplete guards the has-more signal: a walk that filled
// the limit stopped on the caller's bound, not the log's, so older matching
// events may still exist and the caller must keep paging.
func TestTailFullPageIsNotComplete(t *testing.T) {
	const n = 4000
	path := writeTimedLog(t, n, time.Now().UTC())

	scan, _ := tailScanFile(t, path, Filter{}, 10)

	if scan.Complete {
		t.Fatal("a limit-filled tail must not report Complete")
	}
	if got, want := len(scan.Events), 10; got != want {
		t.Fatalf("got %d events, want %d", got, want)
	}
}

// TestTailWithoutFloorIsNotComplete is the case that must still fall through
// to the archive-aware read: an unbounded filter exhausts the active file
// without proving anything about the rotated history below it.
func TestTailWithoutFloorIsNotComplete(t *testing.T) {
	path := writeTimedLog(t, 20, time.Now().UTC())

	scan, _ := tailScanFile(t, path, Filter{Type: "rare"}, 500)

	if scan.Complete {
		t.Fatal("a tail with no filter lower bound cannot report Complete")
	}
	if len(scan.Events) == 0 {
		t.Fatal("expected the matching events from the active file")
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

// TestListTailBoundedOnRecorder pins the provider surface the API layer uses.
func TestListTailBoundedOnRecorder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	rec, err := NewFileRecorder(path, io.Discard)
	if err != nil {
		t.Fatalf("NewFileRecorder: %v", err)
	}
	defer rec.Close() //nolint:errcheck
	for i := 0; i < 50; i++ {
		rec.Record(Event{Type: "t", Actor: "a", Subject: fmt.Sprintf("s%d", i)})
	}

	scan, err := rec.ListTailBounded(Filter{AfterSeq: 45}, 100)
	if err != nil {
		t.Fatalf("ListTailBounded: %v", err)
	}
	if !scan.Complete {
		t.Fatal("a cursored tail inside the active file must report Complete")
	}
	if got, want := len(scan.Events), 5; got != want {
		t.Fatalf("got %d events, want %d", got, want)
	}
}

// TestEmptyActiveFileIsNotComplete pins the post-rotation case. An empty
// active events.jsonl is the normal state right after a rotation, and it says
// nothing about the events above the cursor that may still live in the
// archive or the in-flight rotating file. Reporting Complete there would
// strand that whole seq band behind a cursor the handler never mints.
func TestEmptyActiveFileIsNotComplete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	scan, err := ReadFilteredTailBounded(path, Filter{AfterSeq: 5}, 100)
	if err != nil {
		t.Fatalf("ReadFilteredTailBounded: %v", err)
	}
	if scan.Complete {
		t.Fatal("an empty active file must not report Complete: the archive may still hold matches above the cursor")
	}
	if len(scan.Events) != 0 {
		t.Fatalf("got %d events from an empty log", len(scan.Events))
	}
}

// TestMissingActiveFileIsNotComplete is the same guarantee for a log that does
// not exist yet.
func TestMissingActiveFileIsNotComplete(t *testing.T) {
	scan, err := ReadFilteredTailBounded(filepath.Join(t.TempDir(), "absent.jsonl"), Filter{AfterSeq: 5}, 100)
	if err != nil {
		t.Fatalf("ReadFilteredTailBounded: %v", err)
	}
	if scan.Complete {
		t.Fatal("a missing active file must not report Complete")
	}
}
