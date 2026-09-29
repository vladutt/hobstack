package models

import "testing"

func TestValid(t *testing.T) {
	for _, id := range []string{"opus", "sonnet", "haiku", "fable", "claude-opus-4-8", "claude-sonnet-5-5", "claude-haiku-4-5", "claude-fable-5-1"} {
		if !Valid(id) {
			t.Errorf("%q trebuia sa fie valid", id)
		}
	}
	for _, id := range []string{"", "gpt-5", "claude-opus-5-5-20260401", "Opus"} {
		if Valid(id) {
			t.Errorf("%q nu trebuia sa fie valid", id)
		}
	}
}

func TestListShape(t *testing.T) {
	seen := map[string]bool{}
	aliases := 0
	for _, m := range All {
		if m.ID == "" || m.Label == "" || (m.Group != GroupLatest && m.Group != GroupPinned) {
			t.Fatalf("model incomplet: %+v", m)
		}
		if seen[m.ID] {
			t.Fatalf("model duplicat: %s", m.ID)
		}
		seen[m.ID] = true
		if m.Group == GroupLatest {
			aliases++
		}
	}
	if aliases != 4 {
		t.Fatalf("vreau 4 alias-uri (opus, sonnet, haiku, fable), am %d", aliases)
	}
}
