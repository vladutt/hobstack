package team

import (
	"testing"

	"foreman/internal/runner"
	"foreman/internal/store"
)

func runTaskWithDenials(t *testing.T, f *fixture, d []runner.Denial) store.Task {
	t.Helper()
	task := f.task(t, "A", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: store.RunDone, Denials: d})
	return task
}

func TestDenialsBecomeAccessRequest(t *testing.T) {
	f := setup(t)
	task := runTaskWithDenials(t, f, []runner.Denial{
		{Tool: "Bash", Detail: "ls /"}, {Tool: "Bash", Detail: "ls /etc"},
		{Tool: "TodoWrite", Detail: "x"}, {Tool: "Task"}, {Tool: "Agent"}, {Tool: "TaskCreate"},
	})
	rs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(rs) != 1 || rs[0].Tool != "Bash" || rs[0].Count != 1 || rs[0].Detail != "ls /" || rs[0].TaskID == nil || *rs[0].TaskID != task.ID {
		t.Fatalf("%+v", rs)
	}
	evs, _ := f.st.ListTaskEvents(task.ID)
	found := false
	for _, e := range evs {
		if e.Kind == "denied" && e.Text == "Rook was denied Bash: ls /" {
			found = true
		}
	}
	if !found {
		t.Fatalf("%+v", evs)
	}
}

func TestDenialsCountAcrossRuns(t *testing.T) {
	f := setup(t)
	runTaskWithDenials(t, f, []runner.Denial{{Tool: "Bash", Detail: "ls /"}})
	f.task(t, "B", f.rook, store.StatusQueued)
	mustReconcile(t, f)
	f.fs.finish(f.fs.last(t, f.rook).ID, runner.Outcome{Status: store.RunFailed, Error: "x", Denials: []runner.Denial{{Tool: "Bash", Detail: "ls /"}}})
	// alt task = alta cerere: aprobarea se da pe task-ul ei
	rs, _ := f.st.ListOpenAccessRequests(f.p.ID)
	if len(rs) != 2 || rs[0].Count != 1 || rs[1].Count != 1 || *rs[0].TaskID == *rs[1].TaskID {
		t.Fatalf("%+v", rs)
	}
}

// toate tool-urile din lista fixa de refuzuri (nu doar cele vechi) sunt ignorate
func TestDenialsIgnoreFixedDenyList(t *testing.T) {
	f := setup(t)
	runTaskWithDenials(t, f, []runner.Denial{{Tool: "TaskUpdate"}, {Tool: "TaskList"}, {Tool: "TaskGet"}})
	if rs, _ := f.st.ListOpenAccessRequests(f.p.ID); len(rs) != 0 {
		t.Fatalf("refuzurile fixe nu sunt cereri de acces: %+v", rs)
	}
}

// un run oprit dupa un refuz (fara report_done) asteapta aprobarea: task-ul ramane In progress, nu merge in review
func TestDeniedRunWaitsForApproval(t *testing.T) {
	f := setup(t)
	f.rook.Tools = []string{"git"}
	f.st.UpdateAgent(f.rook)
	task := runTaskWithDenials(t, f, []runner.Denial{{Tool: "Bash", Detail: "git stash"}})
	f.wantStatus(t, task.Number, store.StatusInProgress)
	if !f.hasMessage(f.rook.ID, "T-1 is waiting for your approval in Needs you.") {
		t.Fatal("lipseste nota")
	}
}
