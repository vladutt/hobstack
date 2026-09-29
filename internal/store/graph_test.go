package store

import "testing"

func TestGraphHelpers(t *testing.T) {
	one, two := int64(1), int64(2)
	team := []Agent{
		{ID: 1, Name: "Coord"},
		{ID: 2, Name: "Sub", ParentID: &one},
		{ID: 3, Name: "Wes", ParentID: &two, ReviewerID: &one},
		{ID: 4, Name: "Solo"},
	}
	want := []Roles{
		{IsLead: true, IsRoot: true, IsReviewer: true},
		{IsLead: true},
		{},
		{IsRoot: true},
	}
	for i, a := range team {
		if got := RolesOf(a, team); got != want[i] {
			t.Errorf("RolesOf(%s) = %+v, vreau %+v", a.Name, got, want[i])
		}
	}
	if c := Children(1, team); len(c) != 1 || c[0].Name != "Sub" {
		t.Fatalf("Children(1) = %+v", c)
	}
	checks := []struct {
		anc, id int64
		want    bool
	}{{1, 3, true}, {2, 3, true}, {3, 1, false}, {4, 3, false}, {3, 3, false}, {1, 1, false}}
	for _, c := range checks {
		if got := IsAncestor(c.anc, c.id, team); got != c.want {
			t.Errorf("IsAncestor(%d, %d) = %v", c.anc, c.id, got)
		}
	}
	if _, ok := FindAgent(team, 9); ok {
		t.Fatal("FindAgent(9) a gasit ceva")
	}
}

func TestDetachedIsNotRoot(t *testing.T) {
	a := Agent{ID: 1, Detached: true}
	if r := RolesOf(a, []Agent{a}); r.IsRoot {
		t.Fatalf("nelegat marcat radacina: %+v", r)
	}
}
