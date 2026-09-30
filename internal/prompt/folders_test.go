package prompt

import (
	"encoding/json"
	"runtime"
	"slices"
	"strings"
	"testing"

	"foreman/internal/store"
)

func probe() store.Project {
	return store.Project{Name: "Probe", Goal: "Ship v2", Context: "One product, two parts.", Folders: []store.Folder{
		{ID: 1, Name: "Client", Path: "/code/probe-client", Context: "Go + SQLite"},
		{ID: 2, Name: "Landing", Path: "/code/probe-landing"},
	}}
}

func TestSystemListsFolders(t *testing.T) {
	two := int64(2)
	got := System(probe(), store.Agent{Name: "Lia", Role: "Writer", FolderID: &two}, Links{BossName: "Ana"})
	for _, want := range []string{
		"Folders:\n- Client: /code/probe-client (read only)\n- Landing: /code/probe-landing (your folder)",
		"Shared context:\nOne product, two parts.",
		"Context for Client:\nGo + SQLite",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lipseste %q din:\n%s", want, got)
		}
	}
}

// un proiect cu un singur folder, fara context de folder, are exact prompt-ul de azi
func TestSystemSingleFolderUnchanged(t *testing.T) {
	p := store.Project{Name: "Shop", Goal: "g", Context: "c"}
	before := System(p, store.Agent{Name: "Ana"}, Links{})
	p.Folders = []store.Folder{{ID: 1, Name: "shop", Path: "/code/shop"}}
	if after := System(p, store.Agent{Name: "Ana"}, Links{}); after != before {
		t.Fatalf("prompt schimbat:\n%s\n---\n%s", before, after)
	}
	p.Folders[0].Context = "npm test"
	if got := System(p, store.Agent{Name: "Ana"}, Links{}); strings.Contains(got, "Folders:") || !strings.Contains(got, "Context for shop:\nnpm test") {
		t.Fatalf("un folder cu context:\n%s", got)
	}
}

func TestSettingsScopesWriteToHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cai unix")
	}
	var cfg struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	json.Unmarshal([]byte(Settings([]string{"write"}, "/code/probe-landing")), &cfg)
	for _, want := range []string{"Edit(//code/probe-landing/**)", "Write(//code/probe-landing/**)", "NotebookEdit(//code/probe-landing/**)"} {
		if !slices.Contains(cfg.Permissions.Allow, want) {
			t.Errorf("allow nu contine %s: %v", want, cfg.Permissions.Allow)
		}
	}
	if slices.Contains(cfg.Permissions.Allow, "Edit") || slices.Contains(cfg.Permissions.Allow, "Write") {
		t.Errorf("scrisul fara restrictie nu trebuia permis: %v", cfg.Permissions.Allow)
	}
}

func TestTeamShowsFolder(t *testing.T) {
	got := Team([]Member{{Name: "Rook", Role: "Backend Dev", Status: "idle", Folder: "Client"}})
	if !strings.Contains(got, "- Rook: Backend Dev, idle, folder: Client") {
		t.Fatalf("Team:\n%s", got)
	}
}
