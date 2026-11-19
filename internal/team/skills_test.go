package team

import (
	"strings"
	"testing"

	"hobstack/internal/runner"
	"hobstack/internal/store"
)

func TestSkillOfDenial(t *testing.T) {
	cases := map[string]string{
		`{"skill":"claude-api"}`:                      "claude-api",
		`{"skill":"anthropic-skills:pdf","args":"x"}`: "anthropic-skills:pdf",
		`{"skill":""}`:                                "",
		`not json`:                                    "",
	}
	for in, want := range cases {
		if got := SkillOfDenial("Skill", in); got != want {
			t.Errorf("%s: %q, vrem %q", in, got, want)
		}
	}
	if got := SkillOfDenial("Bash", `{"skill":"x"}`); got != "" {
		t.Errorf("alt tool: %q", got)
	}
}

// instalarea skill-ului in proiect inchide cererile Skill pentru el si trezeste agentul o singura data
func TestSkillAvailableClosesRequestsAndWakes(t *testing.T) {
	f := setup(t)
	task := f.task(t, "Docs", f.rook, store.StatusInProgress)
	f.st.RecordAccessDenial(f.p.ID, f.rook.ID, &task.ID, "Skill", `{"skill":"claude-api"}`, "")
	f.st.RecordAccessDenial(f.p.ID, f.rook.ID, &task.ID, "Skill", `{"skill":"anthropic-skills:claude-api"}`, "")
	f.st.RecordAccessDenial(f.p.ID, f.rook.ID, nil, "Skill", `{"skill":"pdf"}`, "")
	f.st.RecordAccessDenial(f.p.ID, f.rook.ID, nil, "WebFetch", "https://x.dev", "")
	if err := f.tm.SkillAvailable(f.p.ID, "claude-api"); err != nil {
		t.Fatal(err)
	}
	open, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(open) != 2 {
		t.Fatalf("raman pdf si WebFetch: %+v", open)
	}
	msg := `The skill "claude-api" is now installed in this project (added by the user). Use it with the Skill tool as "hobstack-library:claude-api" if you still need it.`
	if !f.hasMessage(f.rook.ID, msg) {
		t.Fatal("Rook nu a fost trezit")
	}
	f.wantStatus(t, task.Number, store.StatusQueued)
	msgs, _ := f.st.ListMessages(f.rook.ID)
	n := 0
	for _, m := range msgs {
		if strings.Contains(m.Text, msg) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("o singura nota pe agent+task, avem %d", n)
	}
}

// un card per skill: argumentele nu fac carduri noi, skill-uri diferite nu se comaseaza
func TestSkillDenialsOneCardPerSkill(t *testing.T) {
	f := setup(t)
	runTaskWithDenials(t, f, []runner.Denial{
		{Tool: "Skill", Detail: `{"skill":"claude-api"}`},
		{Tool: "Skill", Detail: `{"skill":"claude-api","args":"pricing for opus and a very long question that gets cut off at one hundred and twen`},
		{Tool: "Skill", Detail: `{"skill":"pdf"}`},
	})
	rs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(rs) != 2 || rs[0].Detail != `{"skill":"claude-api"}` || rs[1].Detail != `{"skill":"pdf"}` {
		t.Fatalf("%+v", rs)
	}
}
