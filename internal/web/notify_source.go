package web

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"hobstack/internal/notify"
	"hobstack/internal/store"
	"hobstack/internal/team"
)

// viewingTTL: o pagina care nu mai raporteaza (inchisa fara blur) nu mai conteaza dupa atat.
const viewingTTL = 45 * time.Second

// viewer: ce a raportat ultima data o pagina (un tab sau fereastra proprie).
type viewer struct {
	project int64
	focused bool
	at      time.Time
}

// viewers: cine se uita la ce proiect, raportat de pagini prin POST /api/app/viewing.
type viewers struct {
	mu  sync.Mutex
	m   map[string]viewer
	now func() time.Time // nil = time.Now (testele il schimba)
}

func (v *viewers) clock() time.Time {
	if v.now != nil {
		return v.now()
	}
	return time.Now()
}

// setViewing: pagina spune ce proiect are deschis si daca are focus.
func (s *Server) setViewing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Client  string `json:"client"`
		Project int64  `json:"project"`
		Focused bool   `json:"focused"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Client == "" || len(in.Client) > 64 {
		writeErr(w, http.StatusBadRequest, "bad client")
		return
	}
	s.viewing.mu.Lock()
	now := s.viewing.clock()
	if s.viewing.m == nil {
		s.viewing.m = map[string]viewer{}
	}
	for k, v := range s.viewing.m { // paginile inchise de mult ies, ca map-ul sa nu creasca
		if now.Sub(v.at) > viewingTTL {
			delete(s.viewing.m, k)
		}
	}
	s.viewing.m[in.Client] = viewer{project: in.Project, focused: in.Focused, at: now}
	s.viewing.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// Viewing spune daca o pagina cu focus are proiectul deschis (raportat in ultimele 45 s).
func (s *Server) Viewing(projectID int64) bool {
	s.viewing.mu.Lock()
	defer s.viewing.mu.Unlock()
	now := s.viewing.clock()
	for _, v := range s.viewing.m {
		if v.focused && v.project == projectID && now.Sub(v.at) <= viewingTTL {
			return true
		}
	}
	return false
}

// NotifySnapshots: starea fiecarui proiect pentru notificari (aceleasi surse ca "N need you" din UI).
func (s *Server) NotifySnapshots() ([]notify.Snapshot, error) {
	projects, err := s.st.ListProjects()
	if err != nil {
		return nil, err
	}
	out := make([]notify.Snapshot, 0, len(projects))
	for _, p := range projects {
		snap, err := s.notifySnapshot(p)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, nil
}

func (s *Server) notifySnapshot(p store.Project) (notify.Snapshot, error) {
	snap := notify.Snapshot{ProjectID: p.ID, Name: p.Name, Enabled: p.Notify, Needs: []notify.Item{}}
	agents, err := s.st.ListAgents(p.ID)
	if err != nil {
		return snap, err
	}
	tasks, err := s.st.ListTasks(p.ID)
	if err != nil {
		return snap, err
	}
	names := map[int64]string{}
	for _, a := range agents {
		names[a.ID] = a.Name
		if s.team.ActiveTaskID(a.ID) != 0 {
			snap.TaskWork = true
		}
	}
	name := func(id int64) string {
		if n := names[id]; n != "" {
			return n
		}
		return "An agent"
	}
	refs := map[int64]string{}
	for _, t := range tasks {
		refs[t.ID] = t.Ref()
		if t.Status == store.StatusDone {
			snap.Done++
		}
	}
	// pe pauza (Stop team) proiectul e oprit, chiar cu task-uri in coada
	if !p.Paused {
		snap.Busy = s.team.ProjectActive(p.ID)
		for _, a := range agents {
			if snap.Busy {
				break
			}
			if st, _ := s.team.AgentStatus(a, tasks); st == team.StatusQueued {
				snap.Busy = true
			}
		}
	}
	add := func(key, line string) { snap.Needs = append(snap.Needs, notify.Item{Key: key, Line: line}) }

	questions, err := s.st.ListOpenQuestions(p.ID)
	if err != nil {
		return snap, err
	}
	for _, q := range questions {
		add(fmt.Sprintf("q:%d", q.ID), name(q.AgentID)+" asks: "+clip(q.Text, 100))
	}
	stuck, err := s.team.StuckTasks(p.ID, s.team.Now())
	if err != nil {
		return snap, err
	}
	for _, t := range stuck {
		add(fmt.Sprintf("s:%d", t.TaskID), t.Ref+" is stuck")
	}
	access, err := s.st.ListOpenAccessRequests(p.ID)
	if err != nil {
		return snap, err
	}
	for _, r := range access {
		line := name(r.AgentID) + " was denied " + r.Tool
		switch {
		case skillOf(r) != "":
			line = fmt.Sprintf("%s wanted the skill %q", name(r.AgentID), skillOf(r))
		case r.Prefix != "":
			line = name(r.AgentID) + " wants to run " + clip(r.Prefix, 60)
		}
		add(fmt.Sprintf("a:%d", r.ID), line)
	}
	agentReqs, err := s.st.ListOpenAgentRequests(p.ID)
	if err != nil {
		return snap, err
	}
	for _, r := range agentReqs {
		add(fmt.Sprintf("r:%d", r.ID), name(r.LeadID)+" wants to add "+r.Spec.Name)
	}
	work, err := s.st.ListPendingWork(p.ID)
	if err != nil {
		return snap, err
	}
	for _, r := range work {
		from := "A linked project"
		if fp, err := s.st.GetProject(r.FromProject); err == nil {
			from = fp.Name
		}
		add(fmt.Sprintf("w:%d", r.ID), from+" asks for work")
	}
	mcps, err := s.st.ListOpenMCPRequests(p.ID)
	if err != nil {
		return snap, err
	}
	for _, r := range mcps {
		add(fmt.Sprintf("m:%d", r.ID), name(r.AgentID)+" wants the MCP server "+r.Name)
	}
	proposals, err := s.st.ListOpenProposals(p.ID)
	if err != nil {
		return snap, err
	}
	for _, it := range proposals {
		by := it.MadeBy
		if by == "" {
			by = "An agent"
		}
		add(fmt.Sprintf("l:%d", it.ID), by+" proposed "+it.Name+" for the Library")
	}
	// cu Full autonomy pasii lasati utilizatorului nu intra in "need you" (ca in card)
	fills, err := s.st.ListFillIns(p.ID, !p.Autonomy)
	if err != nil {
		return snap, err
	}
	for _, f := range fills {
		ref := refs[f.TaskID]
		if ref == "" {
			ref = fmt.Sprintf("T-%d", f.Number)
		}
		add(fmt.Sprintf("f:%d", f.TaskID), ref+" has steps for you")
	}
	acct, err := s.st.ProjectAccount(p.ID)
	if err != nil {
		return snap, err
	}
	if acct != nil && acct.LastError != "" {
		add(fmt.Sprintf("c:%d", acct.ID), "Claude account "+acct.Name+" failed")
	}
	return snap, nil
}

// clip taie textul la n caractere (pe un rand), cu "…".
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
