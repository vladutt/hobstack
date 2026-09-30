package store

import (
	"errors"
	"testing"
)

func TestSaveStagesLifecycle(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	bob := newAgent(t, s, p.ID, "Bob", &lead.ID)
	vera := newAgent(t, s, p.ID, "Vera", &lead.ID)

	got, err := s.SaveStages(p.ID, []Stage{
		{Name: "Dev", Kind: KindWork, AgentIDs: []int64{bob.ID}},
		{Name: "QA", Kind: KindCheck, AgentIDs: []int64{vera.ID, bob.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Position != 0 || got[1].Position != 1 || got[1].AgentIDs[0] != vera.ID {
		t.Fatalf("salvare: %+v", got)
	}
	// reordonare + redenumire pastreaza id-urile
	dev, qa := got[0], got[1]
	qa.Name = "Test"
	got, err = s.SaveStages(p.ID, []Stage{qa, dev})
	if err != nil || got[0].ID != qa.ID || got[0].Name != "Test" || got[1].ID != dev.ID {
		t.Fatalf("reordonare: %+v %v", got, err)
	}
	// o etapa cu task-uri nu se sterge
	task := Task{ProjectID: p.ID, Title: "x", AssigneeID: bob.ID, StageID: &dev.ID}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	var inUse StageInUseError
	if _, err := s.SaveStages(p.ID, []Stage{qa}); !errors.As(err, &inUse) {
		t.Fatalf("vreau StageInUseError, am %v", err)
	}
	list, _ := s.ListStages(p.ID)
	if len(list) != 2 {
		t.Fatalf("nimic sters dupa eroare: %d", len(list))
	}
	// stage_id si by_user se citesc; SaveTask salveaza assignee si stage
	task.ByUser = true
	task.AssigneeID = vera.ID
	task.StageID = &qa.ID
	if err := s.SaveTask(&task); err != nil {
		t.Fatal(err)
	}
	back, _ := s.GetTask(task.ID)
	if back.AssigneeID != vera.ID || back.StageID == nil || *back.StageID != qa.ID {
		t.Fatalf("SaveTask: %+v", back)
	}
	// stergerea agentului il scoate din etape (cascada)
	if err := s.DeleteAgent(bob.ID); err != nil && !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestCreateTaskByUser(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	task := Task{ProjectID: p.ID, Title: "x", AssigneeID: lead.ID, ByUser: true}
	if err := s.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	if back, _ := s.GetTask(task.ID); !back.ByUser || back.StageID != nil {
		t.Fatalf("%+v", back)
	}
}
