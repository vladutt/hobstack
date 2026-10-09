package store

import (
	"slices"
	"testing"
)

func edit(hunks ...Hunk) TaskChange { return TaskChange{Kind: "edit", Patch: hunks} }

func TestFileMarkersCreate(t *testing.T) {
	m, ok := FileMarkers([]TaskChange{{Kind: "create", Patch: []Hunk{{0, 0, 1, 3, []string{"+a", "+b", "+c"}}}}})
	if !ok || !slices.Equal(m.Added, []int{1, 2, 3}) || len(m.Modified) != 0 || len(m.Deleted) != 0 {
		t.Fatalf("%+v %v", m, ok)
	}
}

func TestFileMarkersReplaceAndDelete(t *testing.T) {
	// linia 2 inlocuita, linia 5 stearsa (fisier de 6 linii)
	m, ok := FileMarkers([]TaskChange{edit(
		Hunk{1, 3, 1, 3, []string{" one", "-two", "+TWO", " three"}},
		Hunk{4, 3, 4, 2, []string{" four", "-five", " six"}},
	)})
	if !ok || !slices.Equal(m.Modified, []int{2}) || !slices.Equal(m.Deleted, []int{5}) || len(m.Added) != 0 {
		t.Fatalf("%+v", m)
	}
}

// al doilea edit adauga doua linii deasupra: marcajul primului coboara cu 2
func TestFileMarkersShift(t *testing.T) {
	m, _ := FileMarkers([]TaskChange{
		edit(Hunk{9, 3, 9, 3, []string{" a", "-b", "+B", " c"}}),   // linia 10 modificata
		edit(Hunk{1, 1, 1, 3, []string{" top", "+new1", "+new2"}}), // doua linii noi dupa linia 1
	})
	if !slices.Equal(m.Added, []int{2, 3}) || !slices.Equal(m.Modified, []int{12}) {
		t.Fatalf("%+v", m)
	}
}

// o linie adaugata, apoi editata din nou: ramane "added"
func TestFileMarkersAddedThenEdited(t *testing.T) {
	m, _ := FileMarkers([]TaskChange{
		edit(Hunk{1, 1, 1, 2, []string{" a", "+b"}}),
		edit(Hunk{1, 2, 1, 2, []string{" a", "-b", "+b2"}}),
	})
	if !slices.Equal(m.Added, []int{2}) || len(m.Modified) != 0 {
		t.Fatalf("%+v", m)
	}
}

func TestFileMarkersNotExact(t *testing.T) {
	// fara numere de linie (fallback fara structuredPatch) sau trunchiat: fara marcaje
	for _, c := range []TaskChange{
		{Kind: "edit", Patch: []Hunk{{0, 0, 0, 0, []string{"-x", "+y"}}}},
		{Kind: "edit", Truncated: true, Patch: []Hunk{{1, 1, 1, 1, []string{"-x", "+y"}}}},
		{Kind: "update"}, // Write peste un fisier existent, fara fragmente
	} {
		if m, ok := FileMarkers([]TaskChange{c}); ok || len(m.Added)+len(m.Modified)+len(m.Deleted) != 0 {
			t.Fatalf("%+v: %+v %v", c, m, ok)
		}
	}
}

// jsdiff (structuredPatch, ce intoarce Claude): la OldLines == 0, OldStart e linia INAINTEA careia vin liniile noi
func TestFileMarkersPureInsert(t *testing.T) {
	// fisier gol -> doua linii
	if m, _ := FileMarkers([]TaskChange{edit(Hunk{1, 0, 1, 2, []string{"+x", "+y"}})}); !slices.Equal(m.Added, []int{1, 2}) {
		t.Fatalf("fisier gol: %+v", m)
	}
	// insert dupa linia 3, fara context
	if m, _ := FileMarkers([]TaskChange{edit(Hunk{4, 0, 4, 1, []string{"+z"}})}); !slices.Equal(m.Added, []int{4}) {
		t.Fatalf("insert: %+v", m)
	}
}
