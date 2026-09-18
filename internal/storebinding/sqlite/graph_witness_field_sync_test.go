package sqlite

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
)

// witnessExemptFields names every beads.Bead field encodeWitnessBead does NOT
// hash, each with the reason it cannot be hashed. It is the exemption list
// TestEncodeWitnessBeadHashesEveryDurableField guards against the struct, so a
// field added to beads.Bead is either hashed by the encoder or listed here --
// never silently unhashed.
//
// The reasons matter as much as the names, and all three are the same reason in
// different clothes: the field is a projection one provider derives and another
// does not, so hashing it would make two stores holding identical logical data
// produce unequal digests and fail an equality proof they should pass. None of
// them is precedent for exempting a field a store durably persists -- a
// destination that dropped one of those hashes EQUAL to its source, which is
// the failure this guard exists to prevent.
var witnessExemptFields = map[string]string{
	"Dependencies": "the bead's own copy of edges the dependency family already hashes authoritatively. Hashing both " +
		"would let one store's decode choice -- whether it populates this field on a List read at all -- change the digest.",
	"IsBlocked": "denormalized readiness mirror recomputed from the dep rows the dependency family hashes. A projection, " +
		"not state, and one backends legitimately populate differently: bd serves its own column, the native stores derive it.",
	"IndefinitelyDeferred": "read-time normalization of bd's richer status vocabulary (beads.normalizedBdReadState), " +
		"re-derived on every read rather than stored. It is json:\"-\", so a store persisting beads as JSON has no such " +
		"column to hold; hashing it would make a bd-backed read and a native read of the same row disagree.",
}

// witnessFieldFixture is a bead with every hashed field set to a value
// distinguishable from its zero, so clearing any one of them is detectable in
// the digest.
//
// Ephemeral is false and NoHistory true on purpose. The two reach the stream
// only through witnessStorageTier, which is a priority switch that reads
// Ephemeral first -- so a fixture with Ephemeral=true would mask NoHistory and
// silently vacuate its mutation below. This shape lets each of the two move the
// tier on its own.
func witnessFieldFixture() beads.Bead {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	deferred := created.Add(72 * time.Hour)
	closed := created.Add(96 * time.Hour)
	priority := 2
	blocked := false
	return beads.Bead{
		ID:                   "gcg-41",
		Title:                "session lifecycle",
		Status:               "closed",
		Type:                 "session",
		Priority:             &priority,
		CreatedAt:            created,
		UpdatedAt:            created.Add(time.Hour),
		Assignee:             "worker-1",
		From:                 "dispatcher",
		ParentID:             "gcg-40",
		Ref:                  "step-3",
		Needs:                []string{"gcg-39"},
		Description:          "the bead body, which is durable domain state",
		Labels:               []string{"gc:session"},
		Metadata:             beads.StringMap{"gc.session_name": "worker-1"},
		Dependencies:         []beads.Dep{{IssueID: "gcg-41", DependsOnID: "gcg-40", Type: "blocks"}},
		Ephemeral:            false,
		NoHistory:            true,
		DeferUntil:           &deferred,
		IsBlocked:            &blocked,
		ClosedAt:             &closed,
		CloseReason:          "shipped",
		Owner:                "me@heyhilton.com",
		CreatedBy:            "dispatcher",
		Revision:             7,
		ClaimFence:           3,
		IndefinitelyDeferred: true,
	}
}

// witnessFieldMutations is one mutation per hashed beads.Bead field: the exact
// loss a destination could suffer in that field. Every entry must move the
// digest, and every Bead field must appear either here or in
// witnessExemptFields.
func witnessFieldMutations() map[string]func(beads.Bead) beads.Bead {
	return map[string]func(beads.Bead) beads.Bead{
		"ID":          func(b beads.Bead) beads.Bead { b.ID = "gcg-42"; return b },
		"Title":       func(b beads.Bead) beads.Bead { b.Title = ""; return b },
		"Status":      func(b beads.Bead) beads.Bead { b.Status = "open"; return b },
		"Type":        func(b beads.Bead) beads.Bead { b.Type = ""; return b },
		"Priority":    func(b beads.Bead) beads.Bead { b.Priority = nil; return b },
		"CreatedAt":   func(b beads.Bead) beads.Bead { b.CreatedAt = time.Time{}; return b },
		"UpdatedAt":   func(b beads.Bead) beads.Bead { b.UpdatedAt = time.Time{}; return b },
		"Assignee":    func(b beads.Bead) beads.Bead { b.Assignee = ""; return b },
		"From":        func(b beads.Bead) beads.Bead { b.From = ""; return b },
		"ParentID":    func(b beads.Bead) beads.Bead { b.ParentID = ""; return b },
		"Ref":         func(b beads.Bead) beads.Bead { b.Ref = ""; return b },
		"Needs":       func(b beads.Bead) beads.Bead { b.Needs = nil; return b },
		"Description": func(b beads.Bead) beads.Bead { b.Description = ""; return b },
		"Labels":      func(b beads.Bead) beads.Bead { b.Labels = nil; return b },
		"Metadata":    func(b beads.Bead) beads.Bead { b.Metadata = nil; return b },
		"Ephemeral":   func(b beads.Bead) beads.Bead { b.Ephemeral = true; return b },
		"NoHistory":   func(b beads.Bead) beads.Bead { b.NoHistory = false; return b },
		"DeferUntil":  func(b beads.Bead) beads.Bead { b.DeferUntil = nil; return b },
		"Revision":    func(b beads.Bead) beads.Bead { b.Revision = 0; return b },
		"ClaimFence":  func(b beads.Bead) beads.Bead { b.ClaimFence = 0; return b },
		// The close pair and the attribution pair are the fields this guard was
		// added for: a destination that dropped them is a destination that
		// cannot report when the row finished or who owned it, and nothing
		// downstream can recover either from the rest of the stream.
		"ClosedAt":    func(b beads.Bead) beads.Bead { b.ClosedAt = nil; return b },
		"CloseReason": func(b beads.Bead) beads.Bead { b.CloseReason = ""; return b },
		"Owner":       func(b beads.Bead) beads.Bead { b.Owner = ""; return b },
		"CreatedBy":   func(b beads.Bead) beads.Bead { b.CreatedBy = ""; return b },
	}
}

// witnessExemptMutations is one mutation per exempt field, each of which must
// leave the digest untouched. Without these the exemption list would be a bare
// assertion: a field could be named exempt while the encoder hashed it anyway.
func witnessExemptMutations() map[string]func(beads.Bead) beads.Bead {
	return map[string]func(beads.Bead) beads.Bead{
		"Dependencies":         func(b beads.Bead) beads.Bead { b.Dependencies = nil; return b },
		"IsBlocked":            func(b beads.Bead) beads.Bead { b.IsBlocked = nil; return b },
		"IndefinitelyDeferred": func(b beads.Bead) beads.Bead { b.IndefinitelyDeferred = false; return b },
	}
}

// witnessBeadDigest hashes one bead through the encoder alone, which is the
// unit this guard is about: the encoder's field coverage, not the store's
// behavior above it.
func witnessBeadDigest(t *testing.T, bead beads.Bead) string {
	t.Helper()
	stream := &canonicalStream{}
	if err := encodeWitnessBead(stream, bead); err != nil {
		t.Fatalf("encodeWitnessBead: %v", err)
	}
	return stream.digest()
}

// TestEncodeWitnessBeadHashesEveryDurableField is the field-sync guard over the
// witness encoder, in the shape of cmd/gc's
// TestBeadCopyDifferenceWitnessesEveryDurableField: every field of beads.Bead is
// either hashed by encodeWitnessBead or explicitly exempted with a reason.
//
// A name check alone would be satisfied by an encoder that named a field and
// wrote nothing, so each hashed field also carries a mutation whose digest must
// MOVE, and each exempt field one whose digest must NOT. That is what makes the
// guard non-vacuous: it fails both when a new field goes unhashed and when an
// existing field silently stops being hashed.
//
// The failure it prevents is specific. SemanticWitness is an equality proof for
// class migration -- two stores holding the same logical data produce equal
// digests -- so a field no one hashes is a field a destination can drop while
// still hashing EQUAL to its source, passing the comparison and earning a
// manifest record that the copy was faithful. The close and attribution fields
// reached beads.Bead without reaching this encoder, which is what this guard
// now makes impossible for the next field.
func TestEncodeWitnessBeadHashesEveryDurableField(t *testing.T) {
	hashed := witnessFieldMutations()
	exemptMutations := witnessExemptMutations()

	var unhashed, doubleBooked []string
	for _, field := range reflect.VisibleFields(reflect.TypeOf(beads.Bead{})) {
		_, exempt := witnessExemptFields[field.Name]
		_, encoded := hashed[field.Name]
		switch {
		case exempt && encoded:
			doubleBooked = append(doubleBooked, field.Name)
		case !exempt && !encoded:
			unhashed = append(unhashed, field.Name)
		}
	}
	sort.Strings(unhashed)
	sort.Strings(doubleBooked)
	if len(unhashed) > 0 {
		t.Fatalf("beads.Bead field(s) %v are neither hashed by encodeWitnessBead nor listed in witnessExemptFields. "+
			"A destination that dropped them would hash equal to its source and pass the equality proof. "+
			"Hash them and bump storebinding.SemanticWitnessAlgorithm, or exempt them with the reason they cannot be hashed", unhashed)
	}
	if len(doubleBooked) > 0 {
		t.Fatalf("beads.Bead field(s) %v are listed as exempt AND carry a hash mutation; the two lists disagree", doubleBooked)
	}
	for name := range witnessExemptFields {
		if _, ok := exemptMutations[name]; !ok {
			t.Fatalf("exempt field %q has no mutation proving the exemption is real", name)
		}
	}

	base := witnessFieldFixture()
	baseline := witnessBeadDigest(t, base)
	for name, mutate := range hashed {
		if witnessBeadDigest(t, mutate(base)) == baseline {
			t.Errorf("a bead that lost %s hashed equal; encodeWitnessBead does not hash that field", name)
		}
	}
	for name, mutate := range exemptMutations {
		if witnessBeadDigest(t, mutate(base)) != baseline {
			t.Errorf("exempt field %s moved the digest after all; %s", name, witnessExemptFields[name])
		}
	}
}
