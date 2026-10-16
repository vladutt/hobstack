package team

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hobstack/internal/mcp"
	"hobstack/internal/schedule"
	"hobstack/internal/store"
)

// Tool-urile MCP pentru sarcinile recurente (list/create/update/delete_recurring), pentru lideri si radacini.
// Schimbarile merg doar intr-un run pornit dintr-un mesaj al utilizatorului in chat, si cu Full autonomy
// (spec: docs/superpowers/specs/2026-10-16-hobstack-lead-recurring-design.md).

// errNotAsked: create / update / delete fara cererea utilizatorului din chat
var errNotAsked = errors.New("only when the user asks for it in chat. Suggest it to the user in your reply instead")

// firstStageName: assignee-ul care inseamna prima etapa (AssigneeID 0)
const firstStageName = "first stage"

// dayNames: zilele acceptate in weekdays (scurt si lung, fara majuscule)
var dayNames = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
	"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday,
}

// recurringLead: apelantul, daca e (inca) lider sau radacina (din DB); write cere si mesajul utilizatorului.
// Se cheama cu t.work tinut.
func (t *Team) recurringLead(c mcp.Caller, write bool) (store.Agent, []store.Agent, error) {
	agents, err := t.st.ListAgents(c.ProjectID)
	if err != nil {
		return store.Agent{}, nil, err
	}
	lead, ok := store.FindAgent(agents, c.AgentID)
	if !ok {
		return store.Agent{}, nil, errors.New("your agent no longer exists")
	}
	if !canAddAgents(lead, agents) {
		return store.Agent{}, nil, errors.New("only a lead or an agent that reports to the user can manage recurring tasks")
	}
	if write {
		if _, fromChat := t.requestOf(c.AgentID, c.RunID); !fromChat {
			return store.Agent{}, nil, errNotAsked
		}
	}
	return lead, agents, nil
}

// inZone: asignatul (0 = prima etapa) e liderul, un agent din echipa lui directa sau, pentru o radacina, prima etapa
func inZone(lead store.Agent, agents []store.Agent, assigneeID int64) bool {
	if assigneeID == 0 {
		return store.RolesOf(lead, agents).IsRoot
	}
	if assigneeID == lead.ID {
		return true
	}
	a, ok := store.FindAgent(agents, assigneeID)
	return ok && a.ParentID != nil && *a.ParentID == lead.ID
}

func zoneError(lead store.Agent, agents []store.Agent) error {
	if store.RolesOf(lead, agents).IsRoot {
		return errors.New("the work can be done only by you, an agent of your direct team or the first stage")
	}
	return errors.New("the work can be done only by you or an agent of your direct team")
}

// assigneeOf: numele dat de agent -> id (gol = liderul, "first stage" = 0)
func assigneeOf(lead store.Agent, agents []store.Agent, name string) (int64, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return lead.ID, nil
	case strings.EqualFold(name, firstStageName):
		return 0, nil
	}
	for _, a := range agents {
		if strings.EqualFold(a.Name, name) {
			return a.ID, nil
		}
	}
	return 0, fmt.Errorf("no agent named %q in this project", name)
}

// scheduleOf: programul din argumentele MCP; zilele devin masca, campurile care nu tin de kind se golesc
func scheduleOf(in mcp.RecurringSchedule) (schedule.Spec, error) {
	s := schedule.Spec{Kind: strings.TrimSpace(in.Kind)}
	switch s.Kind {
	case schedule.KindEveryHours:
		s.EveryHours = in.EveryHours
	case schedule.KindDaily:
		s.At = strings.TrimSpace(in.At)
	case schedule.KindWeekly:
		s.At = strings.TrimSpace(in.At)
		for _, d := range in.Weekdays {
			wd, ok := dayNames[strings.ToLower(strings.TrimSpace(d))]
			if !ok {
				return s, RecurringError(fmt.Sprintf("unknown day %q (use mon, tue, wed, thu, fri, sat, sun)", d))
			}
			s.Weekdays |= 1 << wd
		}
	}
	if err := s.Validate(); err != nil {
		return s, RecurringError(err.Error())
	}
	return s, nil
}

// recurringOf: sarcina R-<id> (sau doar id-ul) din proiectul apelantului; alt proiect = inexistenta
func (t *Team) recurringOf(projectID int64, ref string) (store.RecurringTask, error) {
	s := strings.TrimSpace(ref)
	if len(s) > 2 && strings.EqualFold(s[:2], "R-") {
		s = s[2:]
	}
	notFound := fmt.Errorf("%q is not a recurring task of this project; use an id from list_recurring", ref)
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return store.RecurringTask{}, notFound
	}
	r, err := t.st.GetRecurring(id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && r.ProjectID != projectID) {
		return store.RecurringTask{}, notFound
	}
	return r, err
}

// recurringLine: o sarcina pe un rand, ca in list_recurring
func recurringLine(r store.RecurringTask, agents []store.Agent, hasStages bool) string {
	who := "no one"
	if r.AssigneeID != nil {
		if a, ok := store.FindAgent(agents, *r.AssigneeID); ok {
			who = a.Name
		}
	} else if hasStages {
		who = firstStageName
	}
	parts := []string{fmt.Sprintf("R-%d %q", r.ID, r.Title), r.Schedule.String(), who}
	if r.Enabled {
		parts = append(parts, "on", "next "+r.NextRunAt.In(time.Local).Format("Mon Jan 2 15:04"))
	} else {
		parts = append(parts, "paused")
	}
	if r.AutoAccept {
		parts = append(parts, "auto-accept")
	}
	if r.LastNote != "" {
		parts = append(parts, r.LastNote)
	}
	if r.CreatedBy != nil {
		if a, ok := store.FindAgent(agents, *r.CreatedBy); ok {
			parts = append(parts, "created by "+a.Name)
		}
	}
	return strings.Join(parts, " — ")
}

func (t *Team) hasStages(projectID int64) (bool, error) {
	stages, err := t.st.ListStages(projectID)
	return len(stages) > 0, err
}

func (t *Team) ListRecurringFor(c mcp.Caller) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	_, agents, err := t.recurringLead(c, false)
	if err != nil {
		return "", err
	}
	list, err := t.st.ListRecurring(c.ProjectID)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "No recurring tasks in this project.", nil
	}
	staged, err := t.hasStages(c.ProjectID)
	if err != nil {
		return "", err
	}
	lines := make([]string, len(list))
	for i, r := range list {
		lines[i] = recurringLine(r, agents, staged)
	}
	return strings.Join(lines, "\n"), nil
}

func (t *Team) CreateRecurringFor(c mcp.Caller, in mcp.RecurringArgs) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	lead, agents, err := t.recurringLead(c, true)
	if err != nil {
		return "", err
	}
	who, err := assigneeOf(lead, agents, in.Assignee)
	if err != nil {
		return "", err
	}
	if !inZone(lead, agents, who) {
		return "", zoneError(lead, agents)
	}
	spec, err := scheduleOf(in.Schedule)
	if err != nil {
		return "", err
	}
	by := lead.ID
	r, err := t.createRecurring(c.ProjectID, RecurringInput{Title: in.Title, Description: in.Description, AssigneeID: who,
		Schedule: spec, AutoAccept: in.AutoAccept, Enabled: true, CreatedBy: &by})
	if err != nil {
		return "", err
	}
	staged, err := t.hasStages(c.ProjectID)
	if err != nil {
		return "", err
	}
	return "Created " + recurringLine(r, agents, staged) +
		". Each run becomes a task on the board; the user decides it (or it is accepted automatically with auto-accept).", nil
}

func (t *Team) UpdateRecurringFor(c mcp.Caller, ch mcp.RecurringChange) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	lead, agents, err := t.recurringLead(c, true)
	if err != nil {
		return "", err
	}
	r, err := t.recurringOf(c.ProjectID, ch.ID)
	if err != nil {
		return "", err
	}
	if !inZone(lead, agents, derefID(r.AssigneeID)) {
		return "", fmt.Errorf("R-%d belongs to another team; ask the user to change it in the Recurring panel", r.ID)
	}
	in := RecurringInput{Title: r.Title, Description: r.Description, AssigneeID: derefID(r.AssigneeID),
		Schedule: r.Schedule, AutoAccept: r.AutoAccept, Enabled: r.Enabled}
	changed := false
	if ch.Title != nil {
		in.Title, changed = *ch.Title, true
	}
	if ch.Description != nil {
		in.Description, changed = *ch.Description, true
	}
	// assignee gol = neschimbat (gol inseamna "eu" doar la creare)
	if ch.Assignee != nil && strings.TrimSpace(*ch.Assignee) != "" {
		who, err := assigneeOf(lead, agents, *ch.Assignee)
		if err != nil {
			return "", err
		}
		if !inZone(lead, agents, who) {
			return "", zoneError(lead, agents)
		}
		in.AssigneeID, changed = who, true
	}
	if ch.Schedule != nil {
		spec, err := scheduleOf(*ch.Schedule)
		if err != nil {
			return "", err
		}
		in.Schedule, changed = spec, true
	}
	if ch.AutoAccept != nil {
		in.AutoAccept, changed = *ch.AutoAccept, true
	}
	if ch.Enabled != nil {
		in.Enabled, changed = *ch.Enabled, true
	}
	if !changed {
		return "", errors.New("nothing to change: pass at least one field besides id")
	}
	r, err = t.updateRecurring(r.ID, in)
	if err != nil {
		return "", err
	}
	staged, err := t.hasStages(c.ProjectID)
	if err != nil {
		return "", err
	}
	return "Updated " + recurringLine(r, agents, staged), nil
}

func (t *Team) DeleteRecurringFor(c mcp.Caller, ref string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	lead, agents, err := t.recurringLead(c, true)
	if err != nil {
		return "", err
	}
	r, err := t.recurringOf(c.ProjectID, ref)
	if err != nil {
		return "", err
	}
	if !inZone(lead, agents, derefID(r.AssigneeID)) {
		return "", fmt.Errorf("R-%d belongs to another team; ask the user to change it in the Recurring panel", r.ID)
	}
	if err := t.deleteRecurring(r); err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted R-%d %q. Tasks it already created stay on the board.", r.ID, r.Title), nil
}
