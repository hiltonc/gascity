package api

import (
	"net/http"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

func TestBeadListAllTrueWithLimitReturnsWithoutFullScan(t *testing.T) {
	state := newFakeState(t)
	store := state.stores["myrig"]

	for i := 0; i < 20; i++ {
		b := beads.Bead{Title: "t", Status: "open"}
		if _, err := store.Create(b); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 10; i++ {
		b, err := store.Create(beads.Bead{Title: "closed", Status: "open"})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Close(b.ID); err != nil {
			t.Fatal(err)
		}
	}

	h := newTestCityHandler(t, state)
	rec := getBeads(t, h, cityURL(state, "/beads?all=true&limit=3"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	items, _, next := decodeListBody(t, rec)
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	if next == "" {
		t.Fatal("expected next_cursor for continuation, got empty")
	}
}

func TestBeadListAllTrueRespectsTimeout(t *testing.T) {
	orig := beadListAllReadTimeout
	beadListAllReadTimeout = 0
	t.Cleanup(func() { beadListAllReadTimeout = orig })

	state := newFakeState(t)
	h := newTestCityHandler(t, state)

	rec := getBeads(t, h, cityURL(state, "/beads?all=true&limit=3"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
}
