package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hobstack/internal/config"
	"hobstack/internal/store"
)

func (e *env) makeTopic(t *testing.T, pid int64, name string, agents ...int64) store.BrainstormTopic {
	t.Helper()
	code, raw := e.do(t, "POST", "/api/projects/"+itoa(pid)+"/brainstorm", map[string]any{"name": name, "description": "d", "participants": agents})
	if code != http.StatusCreated {
		t.Fatalf("topic: %d %s", code, raw)
	}
	var tp store.BrainstormTopic
	json.Unmarshal(raw, &tp)
	return tp
}

func TestBrainstormTopicAPI(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	a := e.addWorker(t, p.ID, "Ana")
	other := e.createProject(t, t.TempDir())
	b := e.addWorker(t, other.ID, "Bob")
	base := "/api/projects/" + itoa(p.ID) + "/brainstorm"

	tp := e.makeTopic(t, p.ID, "Pricing", a.ID)
	if code, _ := e.do(t, "POST", base, map[string]any{"name": "Pricing", "participants": []int64{a.ID}}); code != http.StatusConflict {
		t.Fatalf("nume duplicat: %d", code)
	}
	if code, _ := e.do(t, "POST", base, map[string]any{"name": "Other", "participants": []int64{b.ID}}); code != http.StatusBadRequest {
		t.Fatalf("participant din alt proiect: %d", code)
	}
	if code, _ := e.do(t, "POST", base, map[string]any{"name": "Empty", "participants": []int64{}}); code != http.StatusBadRequest {
		t.Fatalf("fara participanti: %d", code)
	}
	code, raw := e.do(t, "GET", base, nil)
	if code != http.StatusOK {
		t.Fatalf("lista: %d %s", code, raw)
	}
	var list struct {
		Topics []struct {
			ID            int64 `json:"id"`
			OpenQuestions int   `json:"openQuestions"`
		} `json:"topics"`
		OpenQuestions int `json:"openQuestions"`
	}
	json.Unmarshal(raw, &list)
	if len(list.Topics) != 1 || list.Topics[0].ID != tp.ID || list.Topics[0].OpenQuestions != 0 || list.OpenQuestions != 0 {
		t.Fatalf("lista: %s", raw)
	}
	// PATCH: arhivare
	if code, raw := e.do(t, "PATCH", "/api/brainstorm/"+itoa(tp.ID), map[string]any{"archived": true}); code != http.StatusOK {
		t.Fatalf("patch: %d %s", code, raw)
	}
	if got, _ := e.st.GetTopic(tp.ID); !got.Archived || got.Name != "Pricing" {
		t.Fatalf("dupa patch: %+v", got)
	}
	if code, _ := e.do(t, "GET", "/api/brainstorm/9999", nil); code != http.StatusNotFound {
		t.Fatalf("topic lipsa: %d", code)
	}
}

func TestBrainstormRoundWithFiles(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	a := e.addWorker(t, p.ID, "Ana")
	b := e.addWorker(t, p.ID, "Bob")
	tp := e.makeTopic(t, p.ID, "Logo", a.ID, b.ID)
	png := []byte("\x89PNG\r\n\x1a\n0000")
	code, raw := upload(t, e, "/api/brainstorm/"+itoa(tp.ID)+"/rounds", map[string]string{"text": "ideas please"}, map[string][]byte{"mock.png": png}, true)
	if code != http.StatusCreated {
		t.Fatalf("runda: %d %s", code, raw)
	}
	var made struct {
		Round store.BrainstormRound  `json:"round"`
		Cards []store.BrainstormCard `json:"cards"`
	}
	json.Unmarshal(raw, &made)
	if len(made.Cards) != 2 || made.Cards[0].Status != store.CardWaiting {
		t.Fatalf("carduri: %s", raw)
	}
	code, raw = e.do(t, "GET", "/api/brainstorm/"+itoa(tp.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("get: %d %s", code, raw)
	}
	var got struct {
		Files  []store.BrainstormFile `json:"files"`
		Rounds []struct {
			Text  string `json:"text"`
			Cards []struct {
				AgentName string `json:"agentName"`
				Busy      string `json:"busy"`
			} `json:"cards"`
		} `json:"rounds"`
	}
	json.Unmarshal(raw, &got)
	if len(got.Files) != 1 || got.Files[0].Name != "mock.png" || len(got.Rounds) != 1 || len(got.Rounds[0].Cards) != 2 || got.Rounds[0].Cards[0].AgentName == "" {
		t.Fatalf("detaliu: %s", raw)
	}
	dir := filepath.Join(config.ProjectBrainstormDir(e.dirs.Projects, p.ID), itoa(tp.ID))
	// Stored nu iese din API: il citim din store
	sf, err := e.st.GetTopicFile(got.Files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, sf.Stored)); err != nil || string(data) != string(png) {
		t.Fatalf("fisier pe disc: %v", err)
	}
	// descarcare
	res, err := http.Get(e.ts.URL + "/api/brainstorm/" + itoa(tp.ID) + "/files/" + itoa(got.Files[0].ID))
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("serve: %v %v", err, res)
	}
	res.Body.Close()
	// stergere
	if code, _ := e.do(t, "DELETE", "/api/brainstorm/"+itoa(tp.ID)+"/files/"+itoa(got.Files[0].ID), nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, sf.Stored)); !os.IsNotExist(err) {
		t.Fatalf("fisierul trebuia sters: %v", err)
	}
	// fara urme de staging
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("ramas in %s: %v", dir, entries)
	}
}

func TestBrainstormSaveOverwrites(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	a := e.addWorker(t, p.ID, "Ana")
	tp := e.makeTopic(t, p.ID, "Big Plan", a.ID)
	_, cards, err := e.tm.NewRound(tp.ID, "go")
	if err != nil {
		t.Fatal(err)
	}
	save := func() string {
		code, raw := e.do(t, "POST", "/api/brainstorm/"+itoa(tp.ID)+"/save", nil)
		if code != http.StatusOK {
			t.Fatalf("save: %d %s", code, raw)
		}
		var out struct{ Name string }
		json.Unmarshal(raw, &out)
		return out.Name
	}
	name := save()
	if name != "brainstorm-"+itoa(tp.ID)+"-big-plan.md" {
		t.Fatalf("nume: %q", name)
	}
	if err := e.st.FinishCard(cards[0].ID, store.CardIdea, "Golden Idea", "text", "", nil); err != nil {
		t.Fatal(err)
	}
	if code, raw := e.do(t, "POST", "/api/brainstorm/cards/"+itoa(cards[0].ID)+"/pick", nil); code != http.StatusNoContent {
		t.Fatalf("pick: %d %s", code, raw)
	}
	save()
	files, _ := e.st.ListProjectFiles(p.ID)
	n := 0
	for _, f := range files {
		if f.Name == name {
			n++
			data, err := os.ReadFile(filepath.Join(config.ProjectFilesDir(e.dirs.Projects, p.ID), f.Stored))
			if err != nil || !strings.Contains(string(data), "Golden Idea") || f.Size != int64(len(data)) {
				t.Fatalf("continut: %v %q size=%d", err, data, f.Size)
			}
		}
	}
	if n != 1 {
		t.Fatalf("fisiere: %d", n)
	}
	// scrierea prin fisier temporar + rename nu lasa urme
	entries, _ := os.ReadDir(config.ProjectFilesDir(e.dirs.Projects, p.ID))
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), ".save-") {
			t.Fatalf("fisier temporar ramas: %s", en.Name())
		}
	}
}

func TestBrainstormCardConflicts(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	a := e.addWorker(t, p.ID, "Ana")
	b := e.addWorker(t, p.ID, "Bob")
	tp := e.makeTopic(t, p.ID, "X", a.ID, b.ID)
	_, cards, err := e.tm.NewRound(tp.ID, "go")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.FinishCard(cards[0].ID, store.CardIdea, "Idea", "t", "", nil); err != nil {
		t.Fatal(err)
	}
	card := func(id int64, action string) string { return "/api/brainstorm/cards/" + itoa(id) + "/" + action }
	if code, _ := e.do(t, "POST", card(cards[0].ID, "answer"), map[string]string{"answer": "yes"}); code != http.StatusConflict {
		t.Fatalf("answer pe idee: %d", code)
	}
	if code, _ := e.do(t, "POST", card(cards[1].ID, "pick"), nil); code != http.StatusConflict {
		t.Fatalf("pick pe waiting: %d", code)
	}
}
