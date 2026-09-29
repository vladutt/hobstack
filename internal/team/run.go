package team

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"foreman/internal/mcp"
	"foreman/internal/prompt"
	"foreman/internal/runner"
	"foreman/internal/store"
)

// startRun porneste un run pentru agent. taskID = 0 inseamna run fara task
// (lead sau mesaj direct). Se apeleaza cu t.work blocat.
func (t *Team) startRun(p store.Project, a store.Agent, agents []store.Agent, taskID int64, turn string) error {
	if info, err := os.Stat(p.Workdir); err != nil || !info.IsDir() {
		return fmt.Errorf("folderul de lucru %s nu exista", p.Workdir)
	}

	// fisierele proiectului: agentul are voie sa citeasca folderul si le vede in tura
	var extraArgs []string
	if dir := t.filesDir(p.ID); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		extraArgs = []string{"--add-dir", dir}
		files, err := t.st.ListProjectFiles(p.ID)
		if err != nil {
			return err
		}
		refs := make([]prompt.FileRef, 0, len(files))
		for _, f := range files {
			refs = append(refs, prompt.FileRef{Name: f.Name, Path: filepath.Join(dir, f.Stored)})
		}
		if block := prompt.FilesBlock(refs); block != "" {
			turn += "\n\n" + block
		}
	}

	system := prompt.System(p, a, prompt.LinksFor(a, agents))
	// Claude tine sesiunile pe cwd, deci un alt folder de lucru inseamna alta sesiune.
	hash := prompt.Hash(p.Workdir + "\n" + system)
	// Claude retine system prompt-ul de la inceputul sesiunii. Daca s-a schimbat
	// (agent sau proiect editat), --resume l-ar ignora, deci pornim sesiune noua.
	resume := a.SessionID != "" && a.PromptHash == hash
	if a.SessionID != "" && !resume {
		t.note(p.ID, a.ID, "Instructiunile sau folderul de lucru s-au schimbat, pornesc o sesiune noua.", true)
	}

	runID := runner.NewID()
	token := newToken()
	if err := t.st.CreateRun(&store.Run{ID: runID, AgentID: a.ID, TaskID: taskID}); err != nil {
		return err
	}

	roles := store.RolesOf(a, agents)
	t.mu.Lock()
	t.tokens[token] = mcp.Caller{ProjectID: p.ID, AgentID: a.ID, RunID: runID, TaskID: taskID,
		IsLead: roles.IsLead, HasParent: a.ParentID != nil, IsReviewer: roles.IsReviewer}
	t.active[a.ID] = activeRun{runID: runID, projectID: p.ID, taskID: taskID, token: token}
	t.mu.Unlock()

	spec := runner.Spec{
		ID:     runID,
		Key:    fmt.Sprintf("agent-%d", a.ID),
		Dir:    p.Workdir,
		Args:   append(t.args(p, a, system, turn, token, resume), extraArgs...),
		Prompt: turn,
	}
	agentID := a.ID
	if _, err := t.runs.Start(spec, func(o runner.Outcome) { t.finish(agentID, hash, o) }); err != nil {
		t.mu.Lock()
		delete(t.tokens, token)
		delete(t.active, a.ID)
		t.mu.Unlock()
		t.st.FinishRun(runID, store.RunFailed, 0, err.Error())
		return err
	}
	t.emit(p.ID, "agent", map[string]int64{"id": a.ID})
	return nil
}

// args construieste linia de comanda pentru claude.
func (t *Team) args(p store.Project, a store.Agent, system, turn, token string, resume bool) []string {
	args := []string{
		"-p", turn,
		"--output-format", "stream-json", "--verbose",
		"--model", a.Model,
		"--append-system-prompt", system,
		// setarile globale ale utilizatorului raman; .claude/settings.json din
		// repo-ul proiectului nu se incarca (permisiunile vin doar de la noi)
		"--setting-sources", "user",
		"--settings", prompt.Settings(a.Tools),
		"--mcp-config", prompt.MCPConfig(t.baseURL + "/mcp/" + token),
		"--strict-mcp-config",
	}
	// bugetul e optional: cu abonament Claude nu platesti per run
	if p.BudgetUSD > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(p.BudgetUSD, 'f', 2, 64))
	}
	if resume {
		args = append(args, "--resume", a.SessionID)
	}
	return args
}

// finish e apelata de runner cand procesul claude s-a terminat.
func (t *Team) finish(agentID int64, hash string, o runner.Outcome) {
	t.work.Lock()
	defer t.work.Unlock()

	t.mu.Lock()
	ar, ok := t.active[agentID]
	if ok && ar.runID == o.RunID {
		delete(t.active, agentID)
		delete(t.tokens, ar.token)
	}
	t.mu.Unlock()
	if !ok || ar.runID != o.RunID {
		log.Printf("run %s terminat, dar nu era activ", o.RunID)
		return
	}

	if err := t.recordOutcome(ar, agentID, hash, o); err != nil {
		log.Printf("finalul run-ului %s: %v", o.RunID, err)
	}
	t.emit(ar.projectID, "agent", map[string]int64{"id": agentID})
	t.Kick(ar.projectID)
}

func (t *Team) recordOutcome(ar activeRun, agentID int64, hash string, o runner.Outcome) error {
	if err := t.st.FinishRun(o.RunID, o.Status, o.CostUSD, o.Error); err != nil {
		return err
	}
	if o.SessionID != "" {
		if err := t.st.SetSession(agentID, o.SessionID, hash); err != nil {
			return err
		}
	}
	a, err := t.st.GetAgent(agentID)
	if err != nil {
		return err
	}
	if text := strings.TrimSpace(o.Result); text != "" {
		m := store.Message{ProjectID: ar.projectID, AgentID: a.ID, Kind: store.KindMsg, Author: a.Name, Text: text, RunID: o.RunID, Delivered: true}
		if err := t.st.AddMessage(&m); err != nil {
			return err
		}
		t.emit(ar.projectID, "message", m)
	}
	if o.Status == store.RunFailed {
		t.note(ar.projectID, a.ID, "Run esuat: "+o.Error, true)
	}
	if ar.taskID == 0 {
		return nil
	}

	task, err := t.st.GetTask(ar.taskID)
	if err != nil {
		return err
	}
	if task.Status != store.StatusInProgress {
		return nil // a apelat report_done sau Stop team l-a pus inapoi in Queued
	}
	agents, err := t.st.ListAgents(ar.projectID)
	if err != nil {
		return err
	}
	subs, err := t.st.Subtasks(task.ID)
	if err != nil {
		return err
	}
	var open []string
	for _, s := range subs {
		if s.Status != store.StatusDone {
			open = append(open, s.Ref())
		}
	}
	// Un sub-lider care a spart task-ul se opreste fara report_done. Task-ul
	// ramane in lucru (oricum s-ar fi terminat run-ul) pana cand toate
	// sub-task-urile ajung in Done; atunci afterDone il pune inapoi in coada.
	if len(open) > 0 {
		if o.Status == store.RunStopped {
			return nil
		}
		return t.note(ar.projectID, a.ID, fmt.Sprintf("%s asteapta sub-task-urile: %s.", task.Ref(), strings.Join(open, ", ")), true)
	}
	switch o.Status {
	case store.RunStopped:
		task.Status = store.StatusQueued
	case store.RunFailed:
		task.Status = store.StatusReview
		task.ReviewStage = store.StageLead
		task.Result = "run esuat: " + o.Error
		boss, hasBoss := decider(task, agents)
		if !hasBoss { // nimeni nu poate decide: decide utilizatorul
			task.Status, task.ReviewStage = store.StatusBlocked, store.StageNone
		}
		if err := t.st.SaveTask(&task); err != nil {
			return err
		}
		t.emit(ar.projectID, "task", task)
		if !hasBoss {
			return nil
		}
		return t.note(ar.projectID, boss.ID, clip(fmt.Sprintf("Run-ul lui %s pe %s a esuat: %s", a.Name, task.Ref(), o.Error)), false)
	default:
		task.Result = strings.TrimSpace(o.Result)
		_, err := t.toReview(&task, a, agents, fmt.Sprintf("%s a terminat %s fara report_done", a.Name, task.Ref()))
		return err
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.emit(ar.projectID, "task", task)
	return nil
}
