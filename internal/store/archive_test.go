package store

import (
	"path/filepath"
	"testing"
)

func TestArchiveAndRestore(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	mk := func(title, status string, parent *int64) Task {
		x := Task{ProjectID: p.ID, Title: title, AssigneeID: lead.ID, ParentTaskID: parent}
		if err := s.CreateTask(&x); err != nil {
			t.Fatal(err)
		}
		x.Status = status
		if err := s.SaveTask(&x); err != nil {
			t.Fatal(err)
		}
		return x
	}
	parent := mk("Parent", StatusInProgress, nil)
	child := mk("Child", StatusInProgress, &parent.ID)
	grand := mk("Grand", StatusQueued, &child.ID)
	other := mk("Other", StatusQueued, nil)

	// o intrebare deschisa pe copil: se inchide la arhivare
	q := Question{ProjectID: p.ID, AgentID: lead.ID, TaskID: &child.ID, Text: "Which DB?"}
	if err := s.AddQuestion(&q); err != nil {
		t.Fatal(err)
	}
	// parintele si copilul asteapta sub-task-uri deschise: niciunul nu se re-pune in coada
	if ids, _ := s.RequeueInProgress(p.ID); len(ids) != 0 {
		t.Fatalf("parinte/copil cu sub-task-uri deschise re-pusi: %v", ids)
	}

	tree, err := s.TaskTree(child.ID)
	if err != nil || len(tree) != 2 || tree[0].ID != child.ID || tree[1].ID != grand.ID {
		t.Fatalf("arbore: %+v %v", tree, err)
	}
	if err := s.ArchiveTasks([]int64{child.ID, grand.ID}, child.ID); err != nil {
		t.Fatal(err)
	}
	// parintele nu mai asteapta un sub-task sters
	if ids, _ := s.RequeueInProgress(p.ID); len(ids) != 1 || ids[0] != parent.ID {
		t.Fatalf("parintele inca asteapta un sub-task sters: %v", ids)
	}
	list, _ := s.ListTasks(p.ID)
	if len(list) != 2 {
		t.Fatalf("ListTasks fara arhivate: %d", len(list))
	}
	if subs, _ := s.Subtasks(parent.ID); len(subs) != 0 {
		t.Fatalf("Subtasks fara arhivate: %+v", subs)
	}
	if open, _ := s.ListOpenQuestions(p.ID); len(open) != 0 {
		t.Fatalf("intrebarea ramasa deschisa: %+v", open)
	}
	got, _ := s.GetTask(child.ID)
	if got.Status != StatusArchived || got.ArchivedFrom != StatusInProgress || got.ArchivedAt == nil || *got.ArchivedRoot != child.ID {
		t.Fatalf("%+v", got)
	}
	roots, _ := s.ListArchivedRoots(p.ID)
	if len(roots) != 1 || roots[0].ID != child.ID || roots[0].Subtasks != 1 {
		t.Fatalf("radacini: %+v", roots)
	}
	if n, _ := s.CountArchivedRoots(p.ID); n != 1 {
		t.Fatalf("numar: %d", n)
	}

	restored, err := s.RestoreGroup(child.ID)
	if err != nil || len(restored) != 2 {
		t.Fatalf("%+v %v", restored, err)
	}
	got, _ = s.GetTask(child.ID)
	// era In progress: revine Queued (run-ul fusese oprit)
	if got.Status != StatusQueued || got.ArchivedFrom != "" || got.ArchivedAt != nil || got.ArchivedRoot != nil {
		t.Fatalf("dupa restore: %+v", got)
	}
	if o, _ := s.GetTask(other.ID); o.Status != StatusQueued {
		t.Fatalf("un task din afara grupului atins: %s", o.Status)
	}
}

// un task arhivat in curs: la Restore revine Queued (run-ul a fost oprit)
func TestRestoreInProgressBecomesQueued(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	x := Task{ProjectID: p.ID, Title: "A", AssigneeID: lead.ID}
	if err := s.CreateTask(&x); err != nil {
		t.Fatal(err)
	}
	x.Status = StatusInProgress
	if err := s.SaveTask(&x); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveTasks([]int64{x.ID}, x.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreGroup(x.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetTask(x.ID); got.Status != StatusQueued {
		t.Fatalf("%s", got.Status)
	}
}

// o etapa se poate sterge chiar daca un task arhivat o refera; la Restore task-ul ramane fara etapa
func TestArchivedTaskDoesNotBlockStageDelete(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	stages, err := s.SaveStages(p.ID, []Stage{{Name: "Build", Kind: KindWork, AgentIDs: []int64{lead.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	x := Task{ProjectID: p.ID, Title: "A", AssigneeID: lead.ID, StageID: &stages[0].ID}
	if err := s.CreateTask(&x); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveTasks([]int64{x.ID}, x.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountStageTasks(stages[0].ID); n != 0 {
		t.Fatalf("arhivatul numarat in etapa: %d", n)
	}
	if _, err := s.SaveStages(p.ID, nil); err != nil {
		t.Fatalf("stergerea etapei blocata: %v", err)
	}
	if _, err := s.RestoreGroup(x.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetTask(x.ID); got.StageID != nil {
		t.Fatalf("etapa stearsa ramasa: %v", *got.StageID)
	}
}

// migrarea 025 modifica CHECK-ul din schema: dupa redeschiderea bazei 'archived' trebuie sa mearga,
// iar schema sa ramana valida
func TestArchivedStatusSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "re.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p, lead := newProject(t, s)
	x := Task{ProjectID: p.ID, Title: "A", AssigneeID: lead.ID}
	if err := s.CreateTask(&x); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ArchiveTasks([]int64{x.ID}, x.ID); err != nil {
		t.Fatal(err)
	}
	var res string
	if err := s.db.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
		t.Fatalf("integrity_check: %q %v", res, err)
	}
}
