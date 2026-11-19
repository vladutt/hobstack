package web

import (
	"encoding/json"
	"strings"
	"testing"

	"hobstack/internal/store"
)

type skillAccess struct {
	ID          int64  `json:"id"`
	Tool        string `json:"tool"`
	Skill       string `json:"skill"`
	SkillItemID int64  `json:"skillItemId"`
}

func (e *libEnv) skillAccess(t *testing.T, pid int64) []skillAccess {
	t.Helper()
	_, raw := e.do(t, "GET", "/api/projects/"+itoa(pid), nil)
	var d struct {
		Access []skillAccess `json:"access"`
	}
	json.Unmarshal(raw, &d)
	return d.Access
}

// un skill refuzat: cardul stie numele si elementul din Library; "install-skill" il pune in proiect si trezeste agentul
func TestAccessSkillInstallFromCard(t *testing.T) {
	e := setupLib(t)
	src := e.project(t)
	it := e.propose(t, store.KindSkill, "claude-api", src.ID, map[string]string{"SKILL.md": "x"}, nil)
	if code, raw := e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil); code != 200 {
		t.Fatalf("publish: %d %s", code, raw)
	}
	p := e.project(t)
	w := e.addWorker(t, p.ID, "Atlas")
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Skill", `{"skill":"claude-api"}`, "")
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Skill", `{"skill":"brand-new"}`, "")
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Skill", `{"skill":"hobstack-library:stamp"}`, "")

	acc := e.skillAccess(t, p.ID)
	if len(acc) != 3 || acc[0].Skill != "claude-api" || acc[0].SkillItemID != it.ID ||
		acc[1].Skill != "brand-new" || acc[1].SkillItemID != 0 || acc[2].Skill != "" {
		t.Fatalf("%+v", acc)
	}
	// fara element in Library nu se poate instala de aici
	if code, _ := e.do(t, "POST", "/api/access/"+itoa(acc[1].ID)+"/install-skill", nil); code != 409 {
		t.Fatalf("fara element: %d", code)
	}
	if code, raw := e.do(t, "POST", "/api/access/"+itoa(acc[0].ID)+"/install-skill", nil); code != 204 {
		t.Fatalf("install-skill: %d %s", code, raw)
	}
	ins, _ := e.st.ListInstalls(p.ID)
	if len(ins) != 1 || ins[0].ItemID != it.ID || !ins[0].Enabled {
		t.Fatalf("installs %+v", ins)
	}
	if acc := e.skillAccess(t, p.ID); len(acc) != 2 {
		t.Fatalf("cardul claude-api ramane: %+v", acc)
	}
	msgs, _ := e.st.ListMessages(w.ID)
	if len(msgs) == 0 || !strings.Contains(msgs[len(msgs)-1].Text, `"hobstack-library:claude-api"`) {
		t.Fatalf("Atlas nu a aflat: %+v", msgs)
	}
}

// instalarea din Library (PUT pe proiect) inchide si ea cardul
func TestAccessSkillClosedByLibraryInstall(t *testing.T) {
	e := setupLib(t)
	src := e.project(t)
	it := e.propose(t, store.KindSkill, "pdf", src.ID, map[string]string{"SKILL.md": "x"}, nil)
	e.do(t, "POST", "/api/library/"+itoa(it.ID)+"/publish", nil)
	p := e.project(t)
	w := e.addWorker(t, p.ID, "Atlas")
	e.st.RecordAccessDenial(p.ID, w.ID, nil, "Skill", `{"skill":"anthropic-skills:pdf"}`, "")
	if code, raw := e.do(t, "PUT", "/api/projects/"+itoa(p.ID)+"/library/"+itoa(it.ID), map[string]any{"installed": true}); code != 204 {
		t.Fatalf("put: %d %s", code, raw)
	}
	if acc := e.skillAccess(t, p.ID); len(acc) != 0 {
		t.Fatalf("%+v", acc)
	}
}
