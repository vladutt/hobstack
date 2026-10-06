package team

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"foreman/internal/mcp"
	"foreman/internal/prompt"
	"foreman/internal/runner"
	"foreman/internal/store"
)

// startRun porneste un run pentru agent. taskID = 0 inseamna run fara task
// (lead sau mesaj direct). Se apeleaza cu t.work blocat.
func (t *Team) startRun(p store.Project, a store.Agent, agents []store.Agent, taskID int64, turn string) error {
	home := store.HomeFolder(a, p.Folders)
	if info, err := os.Stat(home.Path); err != nil || !info.IsDir() {
		return fmt.Errorf("the folder %s (%s) does not exist", home.Name, home.Path)
	}

	// celelalte foldere ale proiectului: agentul le poate citi (scrie doar in al lui)
	var extraArgs []string
	for _, f := range p.Folders {
		if f.ID == home.ID {
			continue
		}
		// un folder sters de pe disc nu trebuie sa strice run-ul
		if info, err := os.Stat(f.Path); err == nil && info.IsDir() {
			extraArgs = append(extraArgs, "--add-dir", f.Path)
		}
	}
	// fisierele proiectului: agentul are voie sa citeasca folderul si le vede in tura
	if dir := t.filesDir(p.ID); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		extraArgs = append(extraArgs, "--add-dir", dir)
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
	hash := prompt.Hash(home.Path + "\n" + system)
	// Claude retine system prompt-ul de la inceputul sesiunii. Daca s-a schimbat
	// (agent sau proiect editat), --resume l-ar ignora, deci pornim sesiune noua.
	resume := a.SessionID != "" && a.PromptHash == hash
	if a.SessionID != "" && !resume {
		t.note(p.ID, a.ID, "The instructions or the working folder changed; starting a new session.", true)
	}

	runID := runner.NewID()
	token := newToken()
	if err := t.st.CreateRun(&store.Run{ID: runID, AgentID: a.ID, TaskID: taskID}); err != nil {
		return err
	}

	roles := store.RolesOf(a, agents)
	stageKind := ""
	stages, err := t.st.ListStages(p.ID)
	if err != nil {
		return err
	}
	if taskID != 0 {
		if task, err := t.st.GetTask(taskID); err == nil {
			if i := stageIndex(stages, task.StageID); i >= 0 {
				stageKind = stages[i].Kind
			}
		}
	}
	// intr-un proiect cu etape, radacinile creeaza task-uri chiar fara copii
	isLead := roles.IsLead || (len(stages) > 0 && roles.IsRoot)
	t.mu.Lock()
	t.tokens[token] = mcp.Caller{ProjectID: p.ID, AgentID: a.ID, RunID: runID, TaskID: taskID,
		IsLead: isLead, HasParent: a.ParentID != nil, IsReviewer: roles.IsReviewer, StageKind: stageKind, HasStages: len(stages) > 0, CanAddAgents: isLead || roles.IsRoot}
	t.active[a.ID] = activeRun{runID: runID, projectID: p.ID, taskID: taskID, token: token}
	t.mu.Unlock()

	spec := runner.Spec{
		ID:     runID,
		Key:    fmt.Sprintf("agent-%d", a.ID),
		Dir:    home.Path,
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
		"--settings", prompt.Settings(a.Tools, store.HomeFolder(a, p.Folders).Path),
		// fara el, un tool nepermis blocheaza run-ul in asteptarea unei aprobari
		"--permission-mode", "dontAsk",
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
		log.Printf("run %s finished, but it was not active", o.RunID)
		return
	}

	if err := t.recordOutcome(ar, agentID, hash, o); err != nil {
		log.Printf("finishing run %s: %v", o.RunID, err)
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
	t.recordDenials(ar, a, o.Denials)
	t.recordChanges(ar, a, o)
	if o.Status == store.RunFailed {
		t.note(ar.projectID, a.ID, "Run failed: "+o.Error, true)
	}
	if ar.taskID == 0 {
		return nil
	}

	task, err := t.st.GetTask(ar.taskID)
	if err != nil {
		return err
	}
	// a apelat report_done, Stop team l-a pus inapoi in Queued, sau (in etape)
	// task-ul a trecut deja la alt agent, care lucreaza acum la el
	if task.Status != store.StatusInProgress || task.AssigneeID != agentID {
		return nil
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
		t.taskEvent(task, "waiting_subtasks", a.Name, "Waiting for subtasks "+strings.Join(open, ", "))
		return t.note(ar.projectID, a.ID, fmt.Sprintf("%s is waiting for its subtasks: %s.", task.Ref(), strings.Join(open, ", ")), true)
	}
	// regula: task-ul asteapta cat timp are orice intrebare deschisa pe el
	waiting, err := t.st.HasOpenQuestionForTask(task.ID)
	if err != nil {
		return err
	}
	switch o.Status {
	case store.RunStopped:
		if waiting {
			return nil // ramane In progress; raspunsul il pune in coada
		}
		task.Status = store.StatusQueued
	case store.RunFailed:
		task.Status = store.StatusReview
		task.ReviewStage = store.StageLead
		task.Result = "run failed: " + o.Error
		// in modul autonom decide radacina; un run esuat nu se accepta niciodata
		// automat: fara decident ramane la utilizator, ca azi
		boss, hasBoss := decider(task, agents, t.autonomyOf(ar.projectID))
		// nimeni nu poate decide: decide utilizatorul (blocat; cele din etape raman la "La lider")
		if !hasBoss && task.StageID == nil && !task.ByUser {
			task.Status, task.ReviewStage = store.StatusBlocked, store.StageNone
		}
		if err := t.st.SaveTask(&task); err != nil {
			return err
		}
		t.taskEvent(task, "failed", a.Name, fmt.Sprintf("%s's run failed: %s", a.Name, o.Error))
		if task.Status == store.StatusBlocked {
			t.taskEvent(task, "blocked", actorForeman, "Blocked: no one can decide, you decide")
		}
		t.emit(ar.projectID, "task", task)
		if !hasBoss {
			return nil
		}
		return t.note(ar.projectID, boss.ID, clip(fmt.Sprintf("%s's run on %s failed: %s", a.Name, task.Ref(), o.Error)), false)
	default:
		// a intrebat utilizatorul (ask_user): task-ul asteapta raspunsul, nu merge in review
		if waiting {
			return t.note(ar.projectID, a.ID, task.Ref()+" is waiting for your answer.", true)
		}
		// run-ul a intrebat, dar utilizatorul a raspuns deja (in timpul run-ului):
		// task-ul revine in coada; raspunsul e deja un mesaj nelivrat pentru agent
		asked, err := t.st.HasQuestionForRun(o.RunID)
		if err != nil {
			return err
		}
		if asked {
			task.Status = store.StatusQueued
			break
		}
		task.Result = strings.TrimSpace(o.Result)
		// body e raportul (raspunsul final al run-ului); destinatarul il alege toReview
		ev := taskNote{kind: "reported", actor: a.Name, text: a.Name + " finished without report_done",
			body: orDefault(task.Result, "(finished without a report)")}
		_, err = t.toReview(&task, a, agents, fmt.Sprintf("%s finished %s without report_done", a.Name, task.Ref()), ev)
		return err
	}
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	if o.Status == store.RunStopped {
		t.taskEvent(task, "requeued", actorForeman, "Stopped; back in the queue")
	}
	t.emit(ar.projectID, "task", task)
	return nil
}

// recordDenials salveaza refuzurile de permisiuni ca cereri de acces (un tool
// distinct o singura data per run) si le trece in istoricul task-ului.
// Ruleaza sub t.work, deci nu ia lock-uri.
func (t *Team) recordDenials(ar activeRun, a store.Agent, denials []runner.Denial) {
	var task *store.Task
	var tid *int64
	if ar.taskID != 0 {
		if tk, err := t.st.GetTask(ar.taskID); err == nil {
			task, tid = &tk, &tk.ID
		}
	}
	seen := map[string]bool{}
	recorded := false
	for _, d := range denials {
		// refuzate intentionat (lista fixa din settings; Task e numele vechi al lui Agent)
		if d.Tool == "Task" || prompt.AlwaysDeniedTool(d.Tool) {
			continue
		}
		if seen[d.Tool] {
			continue
		}
		seen[d.Tool] = true
		if err := t.st.RecordAccessDenial(ar.projectID, a.ID, tid, d.Tool, d.Detail); err != nil {
			log.Printf("agent %d: access denial: %v", a.ID, err)
			continue
		}
		recorded = true
		if task != nil {
			text := fmt.Sprintf("%s was denied %s", a.Name, d.Tool)
			if d.Detail != "" {
				text += ": " + d.Detail
			}
			t.taskEvent(*task, "denied", a.Name, text)
		}
	}
	if recorded {
		t.emit(ar.projectID, "access", map[string]any{"agentId": a.ID})
	}
}

// recordChanges salveaza modificarile de fisiere si comenzile run-ului pe task
// (pentru orice status). Caile devin relative la folderul agentului cand sunt in
// el. Erorile se logheaza. Ruleaza sub t.work, deci nu ia lock-uri.
func (t *Team) recordChanges(ar activeRun, a store.Agent, o runner.Outcome) {
	if ar.taskID == 0 || (len(o.Changes) == 0 && len(o.Commands) == 0) {
		return
	}
	task, err := t.st.GetTask(ar.taskID)
	if err != nil {
		log.Printf("agent %d: task changes: %v", a.ID, err)
		return
	}
	p, err := t.st.GetProject(ar.projectID)
	if err != nil {
		log.Printf("agent %d: task changes: %v", a.ID, err)
		return
	}
	homeFolder := store.HomeFolder(a, p.Folders)
	home := homeFolder.Path
	// cu mai multe foldere, calea relativa primeste prefixul numelui folderului (altfel e ambigua)
	prefix := ""
	if len(p.Folders) > 1 {
		prefix = homeFolder.Name
		if prefix == "" {
			prefix = store.FolderName(homeFolder.Path)
		}
		prefix += "/"
	}

	now := time.Now()
	var cs []store.TaskChange
	for _, c := range o.Changes {
		path := c.Path
		if home != "" {
			if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				path = rel
				if prefix != "" {
					path = prefix + filepath.ToSlash(rel)
				}
			}
		}
		var hunks []store.Hunk
		for _, h := range c.Patch {
			hunks = append(hunks, store.Hunk{OldStart: h.OldStart, OldLines: h.OldLines, NewStart: h.NewStart, NewLines: h.NewLines, Lines: h.Lines})
		}
		cs = append(cs, store.TaskChange{TaskID: task.ID, RunID: o.RunID, AgentID: &a.ID, AgentName: a.Name, At: now,
			Kind: c.Kind, Tool: c.Tool, Path: path, Patch: hunks, Truncated: c.Truncated})
	}
	for _, cmd := range o.Commands {
		cs = append(cs, store.TaskChange{TaskID: task.ID, RunID: o.RunID, AgentID: &a.ID, AgentName: a.Name, At: now,
			Kind: "command", Tool: "Bash", Command: cmd.Text, OK: cmd.OK})
	}
	if err := t.st.AddTaskChanges(cs); err != nil {
		log.Printf("agent %d: task changes: %v", a.ID, err)
		return
	}
	t.emit(ar.projectID, "task", task)
}
