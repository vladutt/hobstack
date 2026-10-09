package team

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"foreman/internal/mcp"
	"foreman/internal/store"
)

// MaxSendBacks: dupa atatea trimiteri inapoi, task-ul se blocheaza si decide utilizatorul.
const MaxSendBacks = 3

var _ mcp.Backend = (*Team)(nil) // compilatorul verifica ca Team implementeaza interfata

// recordRequest: un task creat dintr-un run de chat primeste pasul "requested"
// cu mesajul utilizatorului care a pornit run-ul. Se cheama cu t.work tinut.
func (t *Team) recordRequest(c mcp.Caller, task store.Task, leadName string) {
	if c.TaskID != 0 {
		return // run pe task: task-ul vine din delegare, nu din chat
	}
	if m, ok := t.requestOf(c.AgentID, c.RunID); ok {
		t.taskMsgAt(task, m.CreatedAt, "requested", actorUser, leadName, "Asked in chat", m.Text)
	}
}

func (t *Team) CreateTask(c mcp.Caller, title, description, assignee, continues string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	// flag-urile din Caller pot fi vechi (graful s-a editat in timpul run-ului), deci verificam din DB
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	stages, err := t.st.ListStages(c.ProjectID)
	if err != nil {
		return "", err
	}
	if len(stages) > 0 {
		return t.createStageTask(c, agents, stages, title, description, assignee, continues)
	}
	children := store.Children(c.AgentID, agents)
	if len(children) == 0 {
		return "", errors.New("only a lead (an agent with a direct team) can create tasks")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("title is required")
	}
	w, err := t.st.FindAgentByName(c.ProjectID, strings.TrimSpace(assignee))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	if err != nil || !isChild(children, w.ID) {
		names := make([]string, 0, len(children))
		for _, ch := range children {
			names = append(names, ch.Name)
		}
		return "", fmt.Errorf("%q is not on your direct team. Your direct team: %s", assignee, strings.Join(names, ", "))
	}
	lead, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return "", errors.New("your agent no longer exists")
	}

	threadID, err := t.threadFor(c.ProjectID, continues)
	if err != nil {
		return "", err
	}
	parentID, ambiguous, err := t.parentTaskFor(c)
	if err != nil {
		return "", err
	}
	task := store.Task{ProjectID: c.ProjectID, Title: title, Description: strings.TrimSpace(description),
		AssigneeID: w.ID, CreatorID: &lead.ID, ParentTaskID: parentID, ThreadID: threadID}
	if err := t.st.CreateTask(&task); err != nil {
		return "", err
	}
	t.recordRequest(c, task, lead.Name)
	t.taskMsg(task, "created", lead.Name, w.Name, "Created by "+lead.Name, briefBody(task.Title, task.Description))
	t.taskEvent(task, "assigned", lead.Name, "Assigned to "+w.Name)
	t.note(c.ProjectID, lead.ID, fmt.Sprintf("Created %s for %s: %s", task.Ref(), w.Name, title), true)
	t.note(c.ProjectID, w.ID, fmt.Sprintf("%s assigned by %s: %s", task.Ref(), lead.Name, title), true)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	reply := fmt.Sprintf("Created %s for %s. They will receive it automatically.", task.Ref(), w.Name)
	if ambiguous {
		reply += " Created without a parent task: you have several tasks in progress."
	}
	return reply, nil
}

// createStageTask: intr-un proiect cu etape task-ul intra in prima etapa;
// assignee e optional si trebuie sa fie din acea etapa. Poate crea orice lider
// sau radacina (etapele decid cine lucreaza). Se apeleaza cu t.work blocat.
func (t *Team) createStageTask(c mcp.Caller, agents []store.Agent, stages []store.Stage, title, description, assignee, continues string) (string, error) {
	lead, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return "", errors.New("your agent no longer exists")
	}
	roles := store.RolesOf(lead, agents)
	if !roles.IsLead && !roles.IsRoot {
		return "", errors.New("only a lead or an agent that reports to the user can create tasks")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("title is required")
	}
	first := stages[0]
	who := placeholder(first, lead.ID)
	assigned := "Queued for stage " + first.Name // fara asignat: il ia primul liber din etapa
	to := ""                                     // in coada etapei: fara destinatar anume
	if name := strings.TrimSpace(assignee); name != "" {
		w, err := t.st.FindAgentByName(c.ProjectID, name)
		if err != nil || !slices.Contains(first.AgentIDs, w.ID) {
			return "", fmt.Errorf("%q is not in stage %s (the first stage); leave assignee empty so the first free agent takes it", name, first.Name)
		}
		who = w.ID
		assigned = "Assigned to " + w.Name
		to = w.Name
	}
	threadID, err := t.threadFor(c.ProjectID, continues)
	if err != nil {
		return "", err
	}
	parentID, _, err := t.parentTaskFor(c)
	if err != nil {
		return "", err
	}
	task := store.Task{ProjectID: c.ProjectID, Title: title, Description: strings.TrimSpace(description),
		AssigneeID: who, CreatorID: &lead.ID, ParentTaskID: parentID, StageID: &first.ID, ThreadID: threadID}
	if err := t.st.CreateTask(&task); err != nil {
		return "", err
	}
	t.recordRequest(c, task, lead.Name)
	t.taskMsg(task, "created", lead.Name, to, "Created by "+lead.Name, briefBody(task.Title, task.Description))
	t.taskEvent(task, "assigned", lead.Name, assigned)
	t.note(c.ProjectID, lead.ID, fmt.Sprintf("Created %s in stage %s: %s", task.Ref(), first.Name, title), true)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("Created %s in stage %s. The first free agent in the stage will take it.", task.Ref(), first.Name), nil
}

func isChild(children []store.Agent, id int64) bool {
	for _, ch := range children {
		if ch.ID == id {
			return true
		}
	}
	return false
}

// threadFor gaseste firul de lucru pentru continues ("T-85", "t-85" sau "85"):
// urca pe ParentTaskID pana la radacina arborelui task-ului, apoi ia firul
// radacinii (daca e deja un follow-up) sau radacina insasi. continues gol = fara fir.
// Doar afisare: nicio decizie nu citeste thread_id.
func (t *Team) threadFor(projectID int64, continues string) (*int64, error) {
	ref := strings.TrimSpace(continues)
	if ref == "" {
		return nil, nil
	}
	notOurs := fmt.Errorf("%q is not a task of this project", ref)
	n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimPrefix(ref, "T-"), "t-"))
	if err != nil {
		return nil, notOurs
	}
	// cautarea e pe proiectul apelantului: un task din alt proiect nu e gasit
	task, err := t.st.GetTaskByNumber(projectID, n)
	if errors.Is(err, store.ErrNotFound) {
		return nil, notOurs
	}
	if err != nil {
		return nil, err
	}
	root := task
	seen := map[int64]bool{root.ID: true}
	for root.ParentTaskID != nil {
		p, err := t.st.GetTask(*root.ParentTaskID)
		if errors.Is(err, store.ErrNotFound) {
			break // parinte sters: radacina e ultimul gasit
		}
		if err != nil {
			return nil, err
		}
		if seen[p.ID] {
			break // ciclu (date stricate): ne oprim
		}
		seen[p.ID] = true
		root = p
	}
	if root.ThreadID != nil {
		return root.ThreadID, nil
	}
	return &root.ID, nil
}

// parentTaskFor alege task-ul din care se sparge un sub-task nou: task-ul
// run-ului curent, sau singurul task in lucru al apelantului (run de notificari).
// ambiguous e true cand apelantul are mai multe task-uri in lucru: atunci
// sub-task-ul se creeaza fara parinte, ca sa nu il legam de unul gresit.
func (t *Team) parentTaskFor(c mcp.Caller) (parent *int64, ambiguous bool, err error) {
	if c.TaskID != 0 {
		// run-ul oprit de stergere mai poate apela tool-uri pana moare procesul
		if cur, err := t.st.GetTask(c.TaskID); err == nil && cur.Status == store.StatusArchived {
			return nil, false, fmt.Errorf("%s was deleted by the user", cur.Ref())
		}
		return &c.TaskID, false, nil
	}
	tasks, err := t.st.ListTasks(c.ProjectID)
	if err != nil {
		return nil, false, err
	}
	for _, task := range tasks {
		if task.AssigneeID == c.AgentID && task.Status == store.StatusInProgress {
			if parent != nil {
				return nil, true, nil
			}
			id := task.ID
			parent = &id
		}
	}
	return parent, false, nil
}

func (t *Team) ListTasks(c mcp.Caller, status string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	status = strings.TrimSpace(status)
	switch status {
	case "", store.StatusQueued, store.StatusInProgress, store.StatusReview, store.StatusDone, store.StatusBlocked:
	default:
		return "", fmt.Errorf("unknown status %q", status)
	}
	tasks, err := t.st.ListTasks(c.ProjectID)
	if err != nil {
		return "", err
	}
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	names := map[int64]string{}
	for _, a := range agents {
		names[a.ID] = a.Name
	}

	var b strings.Builder
	for _, task := range tasks {
		if status != "" && task.Status != status {
			continue
		}
		fmt.Fprintf(&b, "%s [%s] %s -> %s\n", task.Ref(), task.Status, task.Title, names[task.AssigneeID])
		if (task.Status == store.StatusReview || task.Status == store.StatusBlocked) && task.Result != "" {
			fmt.Fprintf(&b, "  Result: %s\n", clip(task.Result))
		}
	}
	if b.Len() == 0 {
		return "No tasks.", nil
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// rootOf intoarce radacina agentului: stramosul fara sef (urcand pe parent_id).
// Un agent fara sef (sau cu seful sters) e chiar radacina. false daca agentul nu exista.
func rootOf(agentID int64, agents []store.Agent) (store.Agent, bool) {
	a, ok := store.FindAgent(agents, agentID)
	if !ok {
		return a, false
	}
	seen := map[int64]bool{a.ID: true} // garda contra unui ciclu in date
	for a.ParentID != nil {
		p, ok := store.FindAgent(agents, *a.ParentID)
		if !ok || seen[p.ID] {
			break
		}
		seen[p.ID] = true
		a = p
	}
	return a, true
}

// decider intoarce liderul care decide un task din Review: creatorul, iar
// daca a fost sters, seful asignatului. In modul autonom, unde ar decide
// utilizatorul (task din board sau fara decident) decide radacina
// asignatului, daca nu e chiar asignatul.
func decider(task store.Task, agents []store.Agent, autonomous bool) (store.Agent, bool) {
	if d, ok := baseDecider(task, agents); ok || !autonomous {
		return d, ok
	}
	return autoRoot(task, agents)
}

// autoRoot intoarce radacina asignatului daca e alt agent decat asignatul
// (regula decidentului in modul autonom).
func autoRoot(task store.Task, agents []store.Agent) (store.Agent, bool) {
	if r, ok := rootOf(task.AssigneeID, agents); ok && r.ID != task.AssigneeID {
		return r, true
	}
	return store.Agent{}, false
}

// baseDecider e regula de azi (fara autonomie).
func baseDecider(task store.Task, agents []store.Agent) (store.Agent, bool) {
	if task.ByUser {
		return store.Agent{}, false // creat din board: decide utilizatorul
	}
	if task.CreatorID != nil {
		if a, ok := store.FindAgent(agents, *task.CreatorID); ok {
			return a, true
		}
	}
	if w, ok := store.FindAgent(agents, task.AssigneeID); ok && w.ParentID != nil {
		return store.FindAgent(agents, *w.ParentID)
	}
	return store.Agent{}, false
}

// reviewerOf intoarce reviewer-ul asignatului task-ului, daca are unul care exista.
func reviewerOf(task store.Task, agents []store.Agent) (store.Agent, bool) {
	w, ok := store.FindAgent(agents, task.AssigneeID)
	if !ok || w.ReviewerID == nil {
		return store.Agent{}, false
	}
	return store.FindAgent(agents, *w.ReviewerID)
}

// decisionOwner e decisionOwnerIn fara autonomie: cine decide dupa regulile
// de azi. UserDecide il foloseste ca utilizatorul sa poata decide din board
// si in modul autonom.
func decisionOwner(task store.Task, agents []store.Agent) int64 {
	return decisionOwnerIn(task, agents, false)
}

// decisionOwnerIn intoarce agentul care trebuie sa decida acum un task din
// Review sau Blocked: reviewer-ul in etapa reviewer (daca inca exista),
// altfel decidentul (in modul autonom, radacina in locul utilizatorului).
// Intoarce 0 daca nu decide nimeni.
func decisionOwnerIn(task store.Task, agents []store.Agent, autonomous bool) int64 {
	if task.Status != store.StatusReview && task.Status != store.StatusBlocked {
		return 0
	}
	if task.Status == store.StatusReview && task.ReviewStage == store.StageReviewer {
		if r, ok := reviewerOf(task, agents); ok {
			return r.ID
		}
	}
	// modul autonom: un task blocat il decide radacina (doar ea poate send_back pe Blocked).
	// Blocat iar dupa ce radacina i-a dat inca o incercare (Escalated) inseamna un
	// run esuat (altfel ar fi fost acceptat automat): decide utilizatorul, ca fara autonomie.
	if autonomous && task.Status == store.StatusBlocked {
		if task.Escalated {
			autonomous = false
		} else if r, ok := autoRoot(task, agents); ok {
			return r.ID
		}
	}
	if d, ok := decider(task, agents, autonomous); ok {
		return d.ID
	}
	return 0
}

// autonomyOf citeste comutatorul full autonomy al proiectului (o data per
// apel de tool sau final de run). La eroare, modul e considerat oprit.
func (t *Team) autonomyOf(projectID int64) bool {
	p, err := t.st.GetProject(projectID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			log.Printf("project %d: autonomy: %v", projectID, err)
		}
		return false
	}
	return p.Autonomy
}

// Textele acceptului automat din modul autonom (istoricul task-ului).
const (
	autoAcceptText      = "Accepted automatically (full autonomy)"
	autoAcceptAfterText = "Accepted automatically after repeated send-backs (full autonomy)"
	recurringAcceptText = "Accepted automatically (recurring task)"
)

// acceptText: textul din istoric pentru un task trecut singur in Done.
func acceptText(task store.Task) string {
	if task.AutoAccept {
		return recurringAcceptText
	}
	return autoAcceptText
}

// autoDone: un task fara decident trece singur in Done? Da pentru o sarcina
// recurenta cu auto-accept si, in modul autonom, pentru un task care nu e al
// utilizatorului. Un task creat din board ramane la utilizator chiar si in
// modul autonom: l-a cerut el, deci e de fata sa-l decida.
func autoDone(task store.Task, autonomous bool) bool {
	return task.AutoAccept || (autonomous && !task.ByUser)
}

// markDone pune task-ul in Done (fara sa-l salveze), ca la accept_task.
func markDone(task *store.Task) {
	task.Status, task.ReviewStage, task.StageID = store.StatusDone, store.StageNone, nil
}

// autoAccepted termina acceptul automat al unui task deja salvat in Done:
// istoric, UI, trezirea parintelui (afterDone) si scheduler-ul. Kick nu ia
// t.work, deci se poate chema si cu lock-ul tinut (ca in AcceptTask).
// Se apeleaza cu t.work blocat.
func (t *Team) autoAccepted(task store.Task, text string) {
	t.taskEvent(task, "accepted", actorForeman, text)
	t.emit(task.ProjectID, "task", task)
	// task-ul e deja Done: o eroare la trezirea parintelui nu il strica
	if err := t.afterDone(task); err != nil {
		log.Printf("after auto accept %s: %v", task.Ref(), err)
	}
	t.Kick(task.ProjectID)
}

// noDeciderText e nota pentru asignatul unui task pe care nu il poate decide nimeni.
func noDeciderText(task store.Task, assignee store.Agent) string {
	if assignee.ParentID == nil {
		return fmt.Sprintf("No one can check %s. The user decides from the board (Accept / Unblock).", task.Ref())
	}
	return fmt.Sprintf("No one can check %s: the agent has no boss. Link it to a lead on the canvas.", task.Ref())
}

// canDecide: accept_task si send_back sunt permise creatorului si oricarui
// sef de-al lui. Fara creator (sters), oricarui sef al asignatului. In modul
// autonom, unde ar decide utilizatorul (task din board sau fara decident),
// decide si radacina asignatului.
func canDecide(callerID int64, task store.Task, agents []store.Agent, autonomous bool) bool {
	if autonomous {
		if _, ok := baseDecider(task, agents); !ok {
			if r, ok := autoRoot(task, agents); ok && r.ID == callerID {
				return true
			}
		}
	}
	if task.ByUser {
		return false // creat din board: decide doar utilizatorul
	}
	if task.CreatorID != nil {
		return *task.CreatorID == callerID || store.IsAncestor(callerID, *task.CreatorID, agents)
	}
	return store.IsAncestor(callerID, task.AssigneeID, agents)
}

// toReview muta un task (cu Result deja pus) in Review: la reviewer-ul
// asignatului daca are, altfel la liderul care decide. Intoarce agentul
// notificat (zero daca nu exista). ev (raportul) intra in istoric dupa salvare,
// urmat de blocare daca nu poate decide nimeni. Cu reported = false (run
// terminat fara report_done) task-ul nu trece niciodata singur in Done, ca un
// run esuat: fara decident ramane la utilizator. Se apeleaza cu t.work blocat.
func (t *Team) toReview(task *store.Task, assignee store.Agent, agents []store.Agent, headline string, ev taskNote, reported bool) (store.Agent, error) {
	autonomous := t.autonomyOf(task.ProjectID)
	auto := reported && autoDone(*task, autonomous)
	// task dintr-o etapa terminat fara raport: la lider (sau la utilizator), in aceeasi etapa
	if task.StageID != nil {
		boss, ok := decider(*task, agents, autonomous)
		task.Status, task.ReviewStage = store.StatusReview, store.StageLead
		if !ok && auto {
			markDone(task) // modul autonom (sau auto-accept), nimeni deasupra: Done automat
		}
		if err := t.st.SaveTask(task); err != nil {
			return store.Agent{}, err
		}
		ev.to = whoName(boss, ok) // raportul merge la decident (sau la utilizator)
		t.record(*task, ev)
		if task.Status == store.StatusDone {
			t.autoAccepted(*task, acceptText(*task))
			return store.Agent{}, nil
		}
		t.emit(task.ProjectID, "task", *task)
		if ok {
			t.note(boss.ProjectID, boss.ID, clip(fmt.Sprintf("%s (review): %s", headline, task.Result)), false)
		}
		return boss, nil
	}
	var who store.Agent
	var hasWho bool
	task.Status = store.StatusReview
	if assignee.ReviewerID != nil {
		who, hasWho = store.FindAgent(agents, *assignee.ReviewerID)
	}
	if hasWho {
		task.ReviewStage = store.StageReviewer
	} else {
		task.ReviewStage = store.StageLead
		who, hasWho = decider(*task, agents, autonomous)
	}
	switch {
	case !hasWho && auto:
		// sarcina recurenta cu auto-accept, sau modul autonom si asignatul e
		// radacina (nimeni deasupra): nu mai asteapta utilizatorul
		markDone(task)
	case !hasWho && !task.ByUser:
		// nimeni nu poate decide: nu lasam task-ul blocat in Review, decide utilizatorul
		task.Status = store.StatusBlocked
		task.ReviewStage = store.StageNone
	}
	if err := t.st.SaveTask(task); err != nil {
		return store.Agent{}, err
	}
	ev.to = whoName(who, hasWho) // reviewer-ul sau decidentul; fara ei, utilizatorul
	t.record(*task, ev)
	if task.Status == store.StatusDone {
		t.autoAccepted(*task, acceptText(*task))
		return store.Agent{}, nil
	}
	if task.Status == store.StatusBlocked {
		t.taskEvent(*task, "blocked", actorForeman, "Blocked: no one can check it, you decide")
	}
	switch {
	case !hasWho && task.ByUser:
		// task-ul utilizatorului: decide el din board (Accepta / Trimite inapoi)
	case !hasWho:
		t.note(task.ProjectID, assignee.ID, noDeciderText(*task, assignee), true)
	case task.ReviewStage == store.StageReviewer:
		t.note(who.ProjectID, who.ID, clip(fmt.Sprintf("%s. Check it with review_approve or review_reject: %s", headline, task.Result)), false)
	default:
		t.note(who.ProjectID, who.ID, clip(fmt.Sprintf("%s (review): %s", headline, task.Result)), false)
	}
	t.emit(task.ProjectID, "task", *task)
	return who, nil
}

// afterDone: cand ultimul sub-task al unui task ajunge in Done, task-ul
// parinte revine in Queued la acelasi asignat (sub-liderul). Asa primeste un
// run pe task, cu lista sub-task-urilor, si il raporteaza in sus; finalul
// run-ului (recordOutcome) trateaza orice iesire. Se apeleaza cu t.work blocat.
func (t *Team) afterDone(task store.Task) error {
	// un task cerut de un proiect legat isi trimite raportul inapoi, pe orice drum ar ajunge in Done
	if task.RequestID != nil {
		if err := t.deliverRequest(task); err != nil {
			log.Printf("deliver %s: %v", task.Ref(), err)
		}
	}
	if task.ParentTaskID == nil {
		return nil
	}
	parent, err := t.st.GetTask(*task.ParentTaskID)
	if err != nil || parent.Status != store.StatusInProgress {
		return err
	}
	subs, err := t.st.Subtasks(parent.ID)
	if err != nil {
		return err
	}
	for _, s := range subs {
		if s.Status != store.StatusDone {
			return nil
		}
	}
	// daca sub-liderul lucreaza chiar acum pe parinte, finalul run-ului lui
	// muta task-ul in Review; nu il punem inapoi in coada
	t.mu.Lock()
	ar, busy := t.active[parent.AssigneeID]
	t.mu.Unlock()
	if busy && ar.taskID == parent.ID {
		return nil
	}
	parent.Status = store.StatusQueued
	if err := t.st.SaveTask(&parent); err != nil {
		return err
	}
	t.taskEvent(parent, "requeued", actorForeman, "All subtasks are done; back in the queue")
	t.emit(parent.ProjectID, "task", parent)
	text := fmt.Sprintf("All subtasks for %s are done; %s is back in the queue so you can check it and report it.", parent.Ref(), parent.Ref())
	return t.note(parent.ProjectID, parent.AssigneeID, text, true)
}

// rejectTask e partea comuna a send_back si review_reject: numara respingerea
// si, la MaxSendBacks, blocheaza task-ul si anunta reviewer-ul si decidentul.
// Intoarce true daca task-ul s-a blocat; altfel a ramas Queued, iar apelantul
// scrie notele si trezeste asignatul. ev (respingerea) intra in istoric dupa
// salvare, urmat de blocare daca e cazul. Se apeleaza cu t.work blocat.
//
// In modul autonom blocajul ajunge la radacina asignatului (nu la utilizator).
// Daca task-ul a mai fost deblocat o data de radacina (Escalated) sau nu are o
// radacina care sa decida, nu se mai blocheaza: trece direct in Done (fara
// bucla blocat <-> send_back). Atunci intoarce tot true, cu task.Status = Done.
func (t *Team) rejectTask(task *store.Task, feedback string, agents []store.Agent, extra int64, ev taskNote, autonomous bool) (bool, error) {
	task.SendBacks++
	task.Feedback = feedback
	task.ReviewStage = store.StageNone
	blocked := task.SendBacks >= MaxSendBacks
	root, hasRoot := autoRoot(*task, agents)
	auto := blocked && autonomous && (task.Escalated || !hasRoot)
	failedRun := auto && t.lastRunFailed(task.ID)
	if failedRun {
		// un run esuat nu se accepta niciodata automat: blocat, decide utilizatorul
		// (ca fara autonomie). Escalated ramane, deci decisionOwnerIn il lasa utilizatorului.
		auto, autonomous = false, false
	}
	switch {
	case auto:
		markDone(task)
	case blocked:
		task.Status = store.StatusBlocked
	default:
		task.Status = store.StatusQueued
	}
	if err := t.st.SaveTask(task); err != nil {
		return false, err
	}
	t.record(*task, ev)
	if auto {
		t.autoAccepted(*task, autoAcceptAfterText)
		if hasRoot {
			t.note(task.ProjectID, root.ID, fmt.Sprintf("%s was accepted automatically after repeated send-backs (full autonomy).", task.Ref()), true)
		}
		return true, nil
	}
	if blocked {
		reason := fmt.Sprintf("Blocked after %d send-backs", MaxSendBacks)
		if failedRun {
			reason += ": the last run failed, you decide"
		}
		t.taskEvent(*task, "blocked", actorForeman, reason)
		ids := map[int64]bool{extra: true}
		if w, ok := store.FindAgent(agents, task.AssigneeID); ok && w.ReviewerID != nil {
			ids[*w.ReviewerID] = true
		}
		if d, ok := decider(*task, agents, autonomous); ok {
			ids[d.ID] = true
		}
		if autonomous {
			// radacina decide: nota ei e nelivrata, ca s-o trezeasca
			delete(ids, root.ID)
			delete(ids, 0) // 0 = utilizatorul (send back din board)
			text := fmt.Sprintf("%s is blocked after %d rejections or send-backs. As the root lead in full autonomy, decide: "+
				"accept_task to take it as it is, or send_back with clear feedback for one more try. "+
				"If it is blocked again it will be accepted automatically.", task.Ref(), MaxSendBacks)
			t.note(task.ProjectID, root.ID, text, false)
			others := fmt.Sprintf("%s is blocked after %d rejections or send-backs. %s decides.", task.Ref(), MaxSendBacks, root.Name)
			for id := range ids {
				t.note(task.ProjectID, id, others, true)
			}
		} else {
			text := fmt.Sprintf("%s is blocked after %d rejections or send-backs. The user decides.", task.Ref(), MaxSendBacks)
			for id := range ids {
				t.note(task.ProjectID, id, text, true)
			}
		}
	}
	t.emit(task.ProjectID, "task", *task)
	return blocked, nil
}

// lastRunFailed spune daca ultimul rezultat al task-ului (dintre evenimentele
// "reported" si "failed" din istoric) e un run esuat. Se apeleaza cu t.work blocat.
func (t *Team) lastRunFailed(taskID int64) bool {
	evs, err := t.st.ListTaskEvents(taskID)
	if err != nil {
		log.Printf("task %d: events: %v", taskID, err)
		return false
	}
	for i := len(evs) - 1; i >= 0; i-- {
		switch evs[i].Kind {
		case "failed":
			return true
		case "reported":
			return false
		}
	}
	return false
}

// blockedReply e raspunsul pentru apelant cand rejectTask a intors true:
// task blocat, sau (in modul autonom) acceptat automat.
func blockedReply(task store.Task, agents []store.Agent, autonomous bool) string {
	if task.Status == store.StatusDone {
		return fmt.Sprintf("%s was sent back %d times and was accepted automatically (full autonomy).", task.Ref(), MaxSendBacks)
	}
	// blocat cu Escalated (dupa un run esuat): decide utilizatorul
	if autonomous && !task.Escalated {
		if r, ok := autoRoot(task, agents); ok {
			return fmt.Sprintf("%s was sent back %d times and is now blocked; %s decides.", task.Ref(), MaxSendBacks, r.Name)
		}
	}
	return fmt.Sprintf("%s was sent back %d times and is now blocked. The user decides what happens to it.", task.Ref(), MaxSendBacks)
}

func (t *Team) AcceptTask(c mcp.Caller, ref, note string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	task, err := t.findTask(c.ProjectID, ref)
	if err != nil {
		return "", err
	}
	if !canDecide(c.AgentID, task, agents, t.autonomyOf(c.ProjectID)) {
		return "", fmt.Errorf("only the lead who created %s (or one of their bosses) decides", task.Ref())
	}
	if err := t.checkNotAtReviewer(task, agents); err != nil {
		return "", err
	}
	// checkNotAtReviewer a trecut: task-ul e la lider (sau reviewer-ul a disparut)
	if task.Status != store.StatusReview && task.Status != store.StatusBlocked {
		return "", fmt.Errorf("%s is %s; only tasks in review or blocked can be accepted", task.Ref(), task.Status)
	}
	task.Status = store.StatusDone
	task.ReviewStage = store.StageNone
	task.StageID = nil
	if err := t.st.SaveTask(&task); err != nil {
		return "", err
	}
	if lead, ok := store.FindAgent(agents, c.AgentID); ok {
		t.taskMsg(task, "accepted", lead.Name, agentName(agents, task.AssigneeID), lead.Name+" accepted", orDefault(note, "Accepted"))
	}
	text := "Accepted " + task.Ref()
	if n := strings.TrimSpace(note); n != "" {
		text += ": " + n
	}
	t.note(c.ProjectID, c.AgentID, text, true)
	t.emit(c.ProjectID, "task", task)
	// task-ul e deja Done: o eroare la trezirea parintelui nu face accept-ul esuat
	if err := t.afterDone(task); err != nil {
		log.Printf("after accept %s: %v", task.Ref(), err)
	}
	t.Kick(c.ProjectID)
	return task.Ref() + " is Done.", nil
}

// checkNotAtReviewer refuza decizia liderului cat timp task-ul e la un
// reviewer care exista. Daca asignatul nu mai are reviewer (legatura stearsa
// sau reviewer sters), decide liderul, altfel task-ul ar ramane agatat.
func (t *Team) checkNotAtReviewer(task store.Task, agents []store.Agent) error {
	if task.Status != store.StatusReview || task.ReviewStage != store.StageReviewer {
		return nil
	}
	r, ok := reviewerOf(task, agents)
	if !ok {
		return nil
	}
	return fmt.Errorf("%s is still in review with %s", task.Ref(), r.Name)
}

func (t *Team) SendBack(c mcp.Caller, ref, feedback string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return "", errors.New("feedback is required: say concretely what needs to change")
	}
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	task, err := t.findTask(c.ProjectID, ref)
	if err != nil {
		return "", err
	}
	autonomous := t.autonomyOf(c.ProjectID)
	if !canDecide(c.AgentID, task, agents, autonomous) {
		return "", fmt.Errorf("only the lead who created %s (or one of their bosses) decides", task.Ref())
	}
	if err := t.checkNotAtReviewer(task, agents); err != nil {
		return "", err
	}
	switch {
	case task.Status == store.StatusBlocked && autonomous:
		// modul autonom: radacina poate da task-ului blocat inca o incercare
		if r, ok := autoRoot(task, agents); !ok || r.ID != c.AgentID {
			return "", fmt.Errorf("%s is blocked; in full autonomy only the root lead can send it back", task.Ref())
		}
		// a primit deja o incercare in plus si s-a blocat iar dupa un run esuat
		if task.Escalated {
			return "", fmt.Errorf("%s is blocked again after a failed run; the user decides", task.Ref())
		}
		// rejectTask / sendToStage numara si trimiterea asta, deci dupa ea
		// SendBacks = MaxSendBacks - 1: inca o respingere il blocheaza din nou,
		// iar cu Escalated al doilea blocaj il accepta automat
		task.SendBacks = MaxSendBacks - 2
		task.Escalated = true
	case task.Status != store.StatusReview:
		return "", fmt.Errorf("%s is %s; only tasks in review can be sent back", task.Ref(), task.Status)
	}
	worker, ok := store.FindAgent(agents, task.AssigneeID)
	lead, ok2 := store.FindAgent(agents, c.AgentID)
	if !ok || !ok2 {
		return "", errors.New("the agent no longer exists")
	}
	// proiect cu etape: inapoi in etapa in care era (sau in prima etapa)
	stages, err := t.st.ListStages(c.ProjectID)
	if err != nil {
		return "", err
	}
	if len(stages) > 0 {
		target := stages[0]
		if i := stageIndex(stages, task.StageID); i >= 0 {
			target = stages[i]
		}
		// destinatarul il alege sendToStage (coada etapei sau asignatul)
		sent := taskNote{kind: "sent_back", actor: lead.Name, text: lead.Name + " sent it back: " + feedback, body: feedback}
		blocked, err := t.sendToStage(&task, target, feedback, agents, lead.ID, sent, autonomous)
		if err != nil {
			return "", err
		}
		if blocked {
			return blockedReply(task, agents, autonomous), nil
		}
		t.note(c.ProjectID, lead.ID, fmt.Sprintf("Sent %s back to stage %s: %s", task.Ref(), target.Name, feedback), true)
		t.Kick(c.ProjectID)
		return fmt.Sprintf("%s sent back to stage %s (%d/%d).", task.Ref(), target.Name, task.SendBacks, MaxSendBacks), nil
	}

	sent := taskNote{kind: "sent_back", actor: lead.Name, text: lead.Name + " sent it back: " + feedback, to: worker.Name, body: feedback}
	blocked, err := t.rejectTask(&task, feedback, agents, lead.ID, sent, autonomous)
	if err != nil {
		return "", err
	}
	if blocked {
		return blockedReply(task, agents, autonomous), nil
	}
	t.note(c.ProjectID, lead.ID, fmt.Sprintf("Sent %s back to %s: %s", task.Ref(), worker.Name, feedback), true)
	t.note(c.ProjectID, worker.ID, fmt.Sprintf("%s sent back by %s: %s", task.Ref(), lead.Name, feedback), true)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("%s sent back to %s (%d/%d).", task.Ref(), worker.Name, task.SendBacks, MaxSendBacks), nil
}

func (t *Team) ReportDone(c mcp.Caller, ref, summary string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return "", err
	}
	worker, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return "", errors.New("the agent no longer exists")
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "", errors.New("summary is required")
	}
	var task store.Task
	switch {
	case strings.TrimSpace(ref) == "" && c.TaskID == 0 && worker.ParentID == nil:
		return "", errors.New("you report directly to the user: write the summary in your reply, without report_done")
	case strings.TrimSpace(ref) == "" && c.TaskID == 0:
		return "", errors.New(`say which task you are reporting on: report_done(task: "T-3", summary)`)
	case strings.TrimSpace(ref) == "":
		task, err = t.st.GetTask(c.TaskID)
	default:
		task, err = t.findTask(c.ProjectID, ref)
	}
	if err != nil {
		return "", err
	}
	if task.AssigneeID != worker.ID {
		return "", fmt.Errorf("%s is not assigned to you", task.Ref())
	}
	if task.Status == store.StatusArchived {
		return "", fmt.Errorf("%s was deleted by the user", task.Ref())
	}
	if task.Status != store.StatusInProgress {
		return "", fmt.Errorf("%s is no longer in progress (status %s)", task.Ref(), task.Status)
	}
	// task dintr-o etapa: trece in etapa urmatoare (intr-o etapa de verificare, ca o aprobare)
	if task.StageID != nil {
		stages, err := t.st.ListStages(c.ProjectID)
		if err != nil {
			return "", err
		}
		task.Result = summary
		t.note(c.ProjectID, worker.ID, "Finished "+task.Ref()+" in its stage", true)
		return t.advance(&task, stages, agents, worker.Name, reportedNote(worker, summary), t.autonomyOf(c.ProjectID))
	}
	// radacina livreaza munca ceruta de un proiect legat: nu are sef, trece pe la reviewer sau direct in Done
	if worker.ParentID == nil && task.RequestID != nil {
		return t.finishRequestedTask(&task, worker, agents, summary)
	}
	if worker.ParentID == nil {
		return "", errors.New("you report directly to the user: write the summary in your reply, without report_done")
	}
	subs, err := t.st.Subtasks(task.ID)
	if err != nil {
		return "", err
	}
	var open []string
	for _, s := range subs {
		if s.Status != store.StatusDone {
			open = append(open, s.Ref())
		}
	}
	if len(open) > 0 {
		return "", fmt.Errorf("%s still has open subtasks: %s. Report after they are done.", task.Ref(), strings.Join(open, ", "))
	}

	task.Result = summary
	who, err := t.toReview(&task, worker, agents, worker.Name+" finished "+task.Ref(), reportedNote(worker, summary), true)
	if err != nil {
		return "", err
	}
	t.note(c.ProjectID, worker.ID, "Reported "+task.Ref(), true)
	t.Kick(c.ProjectID)
	if task.Status == store.StatusBlocked {
		return fmt.Sprintf("%s is blocked: no one can check it.", task.Ref()), nil
	}
	if task.Status == store.StatusDone {
		return task.Ref() + " is Done: accepted automatically (full autonomy).", nil
	}
	if who.ID == 0 {
		return task.Ref() + " is in Review.", nil
	}
	return fmt.Sprintf("%s is in Review. %s will check it.", task.Ref(), who.Name), nil
}

// reportedNote e evenimentul "a raportat" al unui report_done; body e raportul.
// Destinatarul il alege helper-ul care muta task-ul (toReview / advance).
func reportedNote(worker store.Agent, summary string) taskNote {
	return taskNote{kind: "reported", actor: worker.Name, text: worker.Name + " reported it done", body: summary}
}

// reviewTarget valideaza apelul unui reviewer si intoarce task-ul, asignatul si lista de agenti.
func (t *Team) reviewTarget(c mcp.Caller, ref string) (store.Task, store.Agent, []store.Agent, error) {
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return store.Task{}, store.Agent{}, nil, err
	}
	task, err := t.findTask(c.ProjectID, ref)
	if err != nil {
		return task, store.Agent{}, nil, err
	}
	worker, ok := store.FindAgent(agents, task.AssigneeID)
	if !ok {
		return task, worker, nil, errors.New("the assignee no longer exists")
	}
	if worker.ReviewerID == nil || *worker.ReviewerID != c.AgentID {
		return task, worker, nil, fmt.Errorf("you do not review the work of %s", worker.Name)
	}
	if task.Status != store.StatusReview || task.ReviewStage != store.StageReviewer {
		return task, worker, nil, fmt.Errorf("%s is not waiting for review (status %s)", task.Ref(), task.Status)
	}
	return task, worker, agents, nil
}

func (t *Team) ReviewApprove(c mcp.Caller, ref, note string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	if task, err := t.findTask(c.ProjectID, ref); err == nil && task.StageID != nil {
		stages, agents, err := t.stageCheckTarget(c, task)
		if err != nil {
			return "", err
		}
		checker, _ := store.FindAgent(agents, c.AgentID)
		if n := strings.TrimSpace(note); n != "" {
			task.Result = strings.TrimSpace(task.Result + "\nReview note: " + n)
		}
		t.note(c.ProjectID, c.AgentID, "Approved "+task.Ref(), true)
		return t.advance(&task, stages, agents, checker.Name, approvedNote(checker, note), t.autonomyOf(c.ProjectID))
	}

	task, worker, agents, err := t.reviewTarget(c, ref)
	if err != nil {
		return "", err
	}
	reviewer, _ := store.FindAgent(agents, c.AgentID)
	autonomous := t.autonomyOf(c.ProjectID)
	approved := approvedNote(reviewer, note)
	approved.to = actorUser // fara decident (task din board, blocat, Done automat): utilizatorul
	if task.ByUser && task.AutoAccept && !autonomous {
		// sarcina recurenta cu auto-accept: dupa reviewer trece direct in Done
		markDone(&task)
		if err := t.st.SaveTask(&task); err != nil {
			return "", err
		}
		t.record(task, approved)
		t.note(c.ProjectID, reviewer.ID, "Approved "+task.Ref(), true)
		t.autoAccepted(task, recurringAcceptText)
		return fmt.Sprintf("%s was accepted automatically (recurring task).", task.Ref()), nil
	}
	boss, ok := decider(task, agents, autonomous)
	if task.ByUser && (!autonomous || (!ok && !task.AutoAccept)) {
		// creat de utilizator: dupa verificare decide el (si in modul autonom,
		// cand nu-l decide o radacina, adica asignatul e chiar radacina)
		task.ReviewStage = store.StageLead
		if err := t.st.SaveTask(&task); err != nil {
			return "", err
		}
		t.record(task, approved)
		t.note(c.ProjectID, reviewer.ID, "Approved "+task.Ref(), true)
		t.emit(c.ProjectID, "task", task)
		return fmt.Sprintf("%s went to the user for a decision.", task.Ref()), nil
	}
	if !ok && autonomous {
		// modul autonom si asignatul e radacina: nu decide nimeni, Done automat
		markDone(&task)
		if err := t.st.SaveTask(&task); err != nil {
			return "", err
		}
		t.record(task, approved)
		t.autoAccepted(task, acceptText(task))
		t.note(c.ProjectID, reviewer.ID, "Approved "+task.Ref(), true)
		return task.Ref() + " is Done: accepted automatically (full autonomy).", nil
	}
	if !ok {
		task.Status, task.ReviewStage = store.StatusBlocked, store.StageNone
		if err := t.st.SaveTask(&task); err != nil {
			return "", err
		}
		t.record(task, approved)
		t.taskEvent(task, "blocked", actorForeman, "Blocked: no one can decide, you decide")
		t.note(c.ProjectID, worker.ID, noDeciderText(task, worker), true)
		t.emit(c.ProjectID, "task", task)
		return fmt.Sprintf("%s is blocked: no one can decide. The user decides.", task.Ref()), nil
	}
	task.ReviewStage = store.StageLead
	if err := t.st.SaveTask(&task); err != nil {
		return "", err
	}
	approved.to = boss.Name
	t.record(task, approved)
	text := fmt.Sprintf("%s approved %s, done by %s (review): %s", reviewer.Name, task.Ref(), worker.Name, task.Result)
	if n := strings.TrimSpace(note); n != "" {
		text += " Note: " + n
	}
	t.note(c.ProjectID, boss.ID, clip(text), false)
	t.note(c.ProjectID, reviewer.ID, "Approved "+task.Ref(), true)
	t.emit(c.ProjectID, "task", task)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("%s went to %s for a decision.", task.Ref(), boss.Name), nil
}

// approvedNote e evenimentul unei aprobari (reviewer sau etapa de verificare);
// body e nota reviewer-ului, sau "Approved". Destinatarul il pune apelantul.
func approvedNote(r store.Agent, note string) taskNote {
	return taskNote{kind: "approved", actor: r.Name, text: r.Name + " approved", body: orDefault(note, "Approved")}
}

// rejectedNote e evenimentul unei respingeri (reviewer sau etapa de verificare);
// body e feedback-ul. Destinatarul il pune apelantul (sau sendToStage).
func rejectedNote(r store.Agent, feedback string) taskNote {
	return taskNote{kind: "rejected", actor: r.Name, text: r.Name + " rejected: " + feedback, body: feedback}
}

func (t *Team) ReviewReject(c mcp.Caller, ref, feedback string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()

	if task, err := t.findTask(c.ProjectID, ref); err == nil && task.StageID != nil {
		stages, agents, err := t.stageCheckTarget(c, task)
		if err != nil {
			return "", err
		}
		feedback = strings.TrimSpace(feedback)
		if feedback == "" {
			return "", errors.New("feedback is required: say concretely what needs to change")
		}
		back := backStage(stages, task.StageID)
		checker, _ := store.FindAgent(agents, c.AgentID)
		autonomous := t.autonomyOf(c.ProjectID)
		blocked, err := t.sendToStage(&task, back, feedback, agents, c.AgentID, rejectedNote(checker, feedback), autonomous)
		if err != nil {
			return "", err
		}
		if blocked {
			return blockedReply(task, agents, autonomous), nil
		}
		t.note(c.ProjectID, c.AgentID, fmt.Sprintf("Rejected %s: %s", task.Ref(), feedback), true)
		t.Kick(c.ProjectID)
		return fmt.Sprintf("%s sent back to stage %s (%d/%d).", task.Ref(), back.Name, task.SendBacks, MaxSendBacks), nil
	}

	task, worker, agents, err := t.reviewTarget(c, ref)
	if err != nil {
		return "", err
	}
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return "", errors.New("feedback is required: say concretely what needs to change")
	}
	reviewer, _ := store.FindAgent(agents, c.AgentID)
	autonomous := t.autonomyOf(c.ProjectID)
	rejected := rejectedNote(reviewer, feedback)
	rejected.to = worker.Name
	blocked, err := t.rejectTask(&task, feedback, agents, reviewer.ID, rejected, autonomous)
	if err != nil {
		return "", err
	}
	if blocked {
		return blockedReply(task, agents, autonomous), nil
	}
	t.note(c.ProjectID, worker.ID, fmt.Sprintf("%s rejected in review by %s: %s", task.Ref(), reviewer.Name, feedback), true)
	t.note(c.ProjectID, reviewer.ID, fmt.Sprintf("Rejected %s: %s", task.Ref(), feedback), true)
	t.Kick(c.ProjectID)
	return fmt.Sprintf("%s sent back to %s (%d/%d).", task.Ref(), worker.Name, task.SendBacks, MaxSendBacks), nil
}

// findTask accepta "T-3", "t-3" sau "3".
func (t *Team) findTask(projectID int64, ref string) (store.Task, error) {
	clean := strings.TrimSpace(ref)
	clean = strings.TrimPrefix(strings.TrimPrefix(clean, "T-"), "t-")
	n, err := strconv.Atoi(clean)
	if err != nil {
		return store.Task{}, fmt.Errorf("invalid task %q; use the format T-3", ref)
	}
	task, err := t.st.GetTaskByNumber(projectID, n)
	if errors.Is(err, store.ErrNotFound) {
		return store.Task{}, fmt.Errorf("task T-%d does not exist", n)
	}
	if err == nil && task.Status == store.StatusArchived {
		return store.Task{}, fmt.Errorf("%s was deleted by the user", task.Ref())
	}
	return task, err
}

// AskUser salveaza o intrebare catre utilizator ("Needs you"). Agentul se opreste
// dupa ea; raspunsul vine ca mesaj (AnswerQuestion).
func (t *Team) AskUser(c mcp.Caller, question string, options []string) (string, error) {
	// modul autonom: nicio intrebare, agentul decide singur si continua
	if t.autonomyOf(c.ProjectID) {
		return "Full autonomy is on: the user will not answer. Decide yourself, state your assumption, and continue.", nil
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return "", errors.New("question is required")
	}
	if utf8.RuneCountInString(question) > 2000 {
		return "", errors.New("the question is too long: at most 2000 characters")
	}
	opts := []string{}
	for _, o := range options {
		if o = strings.TrimSpace(o); o != "" {
			opts = append(opts, o)
		}
	}
	if len(opts) > 4 {
		return "", errors.New("at most 4 options")
	}
	for _, o := range opts {
		if utf8.RuneCountInString(o) > 60 {
			return "", errors.New("each option must be at most 60 characters")
		}
	}
	t.work.Lock()
	defer t.work.Unlock()
	q := store.Question{ProjectID: c.ProjectID, AgentID: c.AgentID, RunID: c.RunID, Text: question, Options: opts}
	if c.TaskID != 0 {
		if cur, err := t.st.GetTask(c.TaskID); err == nil && cur.Status == store.StatusArchived {
			return "", fmt.Errorf("%s was deleted by the user", cur.Ref())
		}
		id := c.TaskID
		q.TaskID = &id
	}
	if err := t.st.AddQuestion(&q); err != nil {
		return "", err
	}
	if q.TaskID != nil {
		if task, err := t.st.GetTask(*q.TaskID); err == nil {
			if a, err := t.st.GetAgent(c.AgentID); err == nil {
				body := question // intrebarea, apoi optiunile pe randuri
				for _, o := range opts {
					body += "\n- " + o
				}
				t.taskMsg(task, "asked", a.Name, actorUser, a.Name+" asked you: "+question, body)
			}
		}
	}
	t.note(c.ProjectID, c.AgentID, clip("Asked you: "+question), true)
	t.emit(c.ProjectID, "question", q)
	return "Question sent to the user. Stop now; the answer will arrive as a message.", nil
}
