package team

import (
	"strings"
	"testing"

	"hobstack/internal/store"
)

// threadOf: firul task-ului cu numarul dat (0 = fara fir).
func (f *fixture) threadOf(t *testing.T, number int) int64 {
	t.Helper()
	task, err := f.st.GetTaskByNumber(f.p.ID, number)
	if err != nil {
		t.Fatal(err)
	}
	if task.ThreadID == nil {
		return 0
	}
	return *task.ThreadID
}

func TestCreateTaskContinues(t *testing.T) {
	f := setup(t)
	lc := f.leadCaller()
	start := f.task(t, "Audit", f.rook, store.StatusDone) // T-1
	sub := store.Task{ProjectID: f.p.ID, Title: "Audit part", AssigneeID: f.vera.ID, CreatorID: &f.lead.ID, ParentTaskID: &start.ID}
	if err := f.st.CreateTask(&sub); err != nil { // T-2, sub-task al lui T-1
		t.Fatal(err)
	}

	// fara continues: fara fir
	if _, err := f.tm.CreateTask(lc, "Other", "", "Rook", ""); err != nil { // T-3
		t.Fatal(err)
	}
	if got := f.threadOf(t, 3); got != 0 {
		t.Fatalf("fara continues, fir = %d", got)
	}

	// continues pe radacina, in toate formele acceptate
	for i, ref := range []string{"T-1", " t-1 ", "1"} {
		if _, err := f.tm.CreateTask(lc, "Fix", "", "Rook", ref); err != nil {
			t.Fatalf("%q: %v", ref, err)
		}
		if got := f.threadOf(t, 4+i); got != start.ID {
			t.Fatalf("%q: fir = %d, vreau %d", ref, got, start.ID)
		}
	}

	// continues pe un sub-task: firul e radacina arborelui lui
	if _, err := f.tm.CreateTask(lc, "Fix part", "", "Vera", "T-2"); err != nil { // T-7
		t.Fatal(err)
	}
	if got := f.threadOf(t, 7); got != start.ID {
		t.Fatalf("prin sub-task: fir = %d, vreau %d", got, start.ID)
	}

	// continues pe un follow-up (deja in fir): acelasi fir
	if _, err := f.tm.CreateTask(lc, "Fix again", "", "Rook", "T-4"); err != nil { // T-8
		t.Fatal(err)
	}
	if got := f.threadOf(t, 8); got != start.ID {
		t.Fatalf("prin follow-up: fir = %d, vreau %d", got, start.ID)
	}

	// un sub-task al unui follow-up: urcam pana la radacina lui (T-4), apoi firul ei
	t4, _ := f.st.GetTaskByNumber(f.p.ID, 4)
	child := store.Task{ProjectID: f.p.ID, Title: "Fix child", AssigneeID: f.vera.ID, CreatorID: &f.lead.ID, ParentTaskID: &t4.ID}
	if err := f.st.CreateTask(&child); err != nil { // T-9
		t.Fatal(err)
	}
	if _, err := f.tm.CreateTask(lc, "Fix deep", "", "Rook", "T-9"); err != nil { // T-10
		t.Fatal(err)
	}
	if got := f.threadOf(t, 10); got != start.ID {
		t.Fatalf("prin sub-task de follow-up: fir = %d, vreau %d", got, start.ID)
	}
}

func TestCreateTaskContinuesUnknown(t *testing.T) {
	f := setup(t)
	// un task cu numarul 5 exista doar in alt proiect
	p2 := store.Project{Name: "Other", Workdir: t.TempDir()}
	lead2, err := f.st.CreateProject(&p2)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		task := store.Task{ProjectID: p2.ID, Title: "X", AssigneeID: lead2.ID}
		if err := f.st.CreateTask(&task); err != nil {
			t.Fatal(err)
		}
	}
	for _, ref := range []string{"T-5", " T-85 ", "abc"} {
		_, err := f.tm.CreateTask(f.leadCaller(), "Fix", "", "Rook", ref)
		want := `"` + strings.TrimSpace(ref) + `" is not a task of this project`
		if err == nil || err.Error() != want {
			t.Fatalf("%q: eroare %v, vreau %s", ref, err, want)
		}
	}
	if tasks, _ := f.st.ListTasks(f.p.ID); len(tasks) != 0 {
		t.Fatalf("la eroare nu se creeaza nimic: %+v", tasks)
	}
}

func TestCreateTaskContinuesInStages(t *testing.T) {
	f, _, _ := stagesFixture(t)
	lc := f.leadCaller()
	if _, err := f.tm.CreateTask(lc, "Audit", "", "", ""); err != nil { // T-1
		t.Fatal(err)
	}
	if _, err := f.tm.CreateTask(lc, "Fix", "", "", "T-1"); err != nil { // T-2
		t.Fatal(err)
	}
	first, _ := f.st.GetTaskByNumber(f.p.ID, 1)
	if got := f.threadOf(t, 2); got != first.ID {
		t.Fatalf("cu etape: fir = %d, vreau %d", got, first.ID)
	}
	if got := f.threadOf(t, 1); got != 0 {
		t.Fatalf("task-ul care porneste firul nu are fir: %d", got)
	}
	if _, err := f.tm.CreateTask(lc, "Fix", "", "", "T-9"); err == nil || err.Error() != `"T-9" is not a task of this project` {
		t.Fatalf("cu etape, task necunoscut: %v", err)
	}
}
