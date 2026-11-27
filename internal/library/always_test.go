package library

import (
	"strings"
	"testing"
)

// alwaysLine: linia "Always-on skills" din build ("" daca lipseste).
func alwaysLine(lines []string) string {
	for _, l := range lines {
		if strings.HasPrefix(l, "Always-on skills:") {
			return l
		}
	}
	return ""
}

func TestBuildAlwaysLoad(t *testing.T) {
	st, lib, src := newEnv(t)
	p := project(t, st)
	short, _ := publishDefSkill(t, st, lib, src, "short")
	publishDefSkill(t, st, lib, src, "other")
	for _, n := range []int64{short.ID} {
		if err := st.SetLibraryDefault(n, true); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := lib.Build(p.ID)
	if l := alwaysLine(b.TurnLines); l != "" {
		t.Fatalf("fara flag nu trebuia linia: %q", l)
	}
	// flag-ul global
	if err := st.SetItemAlwaysLoad(short.ID, true); err != nil {
		t.Fatal(err)
	}
	b, _ = lib.Build(p.ID)
	if l := alwaysLine(b.TurnLines); !strings.Contains(l, "hobstack-library:short") || strings.Contains(l, "other") {
		t.Fatalf("linia globala gresita: %q", l)
	}
	// proiectul il opreste
	off := false
	if err := st.SetProjectAlwaysLoad(p.ID, short.ID, &off); err != nil {
		t.Fatal(err)
	}
	b, _ = lib.Build(p.ID)
	if l := alwaysLine(b.TurnLines); l != "" {
		t.Fatalf("proiectul l-a oprit, dar linia a ramas: %q", l)
	}
	// proiectul il porneste fara flag global; nil = inapoi la global
	st.SetItemAlwaysLoad(short.ID, false)
	on := true
	st.SetProjectAlwaysLoad(p.ID, short.ID, &on)
	b, _ = lib.Build(p.ID)
	if !strings.Contains(alwaysLine(b.TurnLines), "hobstack-library:short") {
		t.Fatalf("alegerea proiectului nu s-a aplicat: %v", b.TurnLines)
	}
	st.SetProjectAlwaysLoad(p.ID, short.ID, nil)
	b, _ = lib.Build(p.ID)
	if l := alwaysLine(b.TurnLines); l != "" {
		t.Fatalf("dupa stergerea alegerii trebuia flag-ul global (oprit): %q", l)
	}
	// un skill oprit in proiect nu se cere, chiar cu flag
	st.SetItemAlwaysLoad(short.ID, true)
	vs, _ := st.ListLibraryVersions(short.ID)
	st.DisableInstall(p.ID, short.ID, vs[0].ID)
	b, _ = lib.Build(p.ID)
	if l := alwaysLine(b.TurnLines); l != "" {
		t.Fatalf("skill oprit, dar cerut: %q", l)
	}
}
