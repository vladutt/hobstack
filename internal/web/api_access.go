package web

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"hobstack/internal/prompt"
	"hobstack/internal/store"
	"hobstack/internal/team"
)

// accessView e o cerere de acces deschisa, asa cum o vede UI-ul.
type accessView struct {
	ID        int64     `json:"id"`
	AgentID   int64     `json:"agentId"`
	AgentName string    `json:"agentName"`
	TaskID    *int64    `json:"taskId"`
	Ref       string    `json:"ref"` // T-3, gol daca refuzul n-a venit de pe un task
	Tool      string    `json:"tool"`
	Detail    string    `json:"detail"`
	Prefix    string    `json:"prefix"` // gol = cerere de tool; altfel prefixul de comanda pe care il poti permite/bloca
	Count     int       `json:"count"`
	UpdatedAt time.Time `json:"updatedAt"`
	// CanEnableShell: tool-ul refuzat e Bash, agentul nu are inca "shell" si
	// comanda nu e in refuzurile fixe (acolo Shell nu ar ajuta)
	CanEnableShell bool `json:"canEnableShell"`
	// CanEnablePackages: comanda refuzata instaleaza pachete (npm install...) si
	// agentul nu are inca "packages"
	CanEnablePackages bool   `json:"canEnablePackages"`
	Kind              string `json:"kind"`     // "" (tool) / "ask" / "command"
	HasShell          bool   `json:"hasShell"` // fara Shell, "Allow in this project" nu are efect
	// Skill: numele skill-ului refuzat (fara prefixul plugin-ului), daca se poate rezolva prin instalare;
	// gol pentru skill-urile din Library (hobstack-library:x refuzat = skill cu allowed-tools, instalarea nu ajuta)
	Skill string `json:"skill,omitempty"`
	// SkillItemID: skill-ul cu acest nume din Library (0 = nu e acolo; se adauga din pagina Add skill)
	SkillItemID int64 `json:"skillItemId,omitempty"`
}

// skillOf: numele de pus pe card pentru un refuz Skill ("" = fara buton de instalare).
func skillOf(r store.AccessRequest) string {
	name := team.SkillOfDenial(r.Tool, r.Detail)
	if name == "" || strings.HasPrefix(name, "hobstack-library:") {
		return ""
	}
	return team.SkillBase(name)
}

// accessViews intoarce cererile deschise ale proiectului (lista e mereu non-nil).
func (s *Server) accessViews(projectID int64, agents []store.Agent, tasks []store.Task) ([]accessView, error) {
	reqs, err := s.st.ListOpenAccessRequests(projectID)
	if err != nil {
		return nil, err
	}
	rules, err := s.team.EffectiveCommandRules(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]accessView, 0, len(reqs))
	for _, r := range reqs {
		v := accessView{ID: r.ID, AgentID: r.AgentID, TaskID: r.TaskID, Tool: r.Tool, Detail: r.Detail, Prefix: r.Prefix, Kind: r.Kind, Count: r.Count, UpdatedAt: r.UpdatedAt}
		for _, a := range agents {
			if a.ID == r.AgentID {
				hasShell := slices.Contains(a.Tools, "shell")
				v.AgentName, v.HasShell = a.Name, hasShell
				// Enable Shell ajuta la o cerere de tool Bash (daca nu e refuz fix) si la o comanda calculata
				v.CanEnableShell = r.Tool == "Bash" && !hasShell &&
					(r.Kind == store.AccessCommand || (r.Kind == store.AccessTool && !prompt.AlwaysDeniedBash(r.Detail, a.Tools, rules)))
				v.CanEnablePackages = r.Kind == store.AccessTool && r.Tool == "Bash" && !slices.Contains(a.Tools, "packages") && prompt.NeedsPackages(r.Detail)
			}
		}
		if v.Skill = skillOf(r); v.Skill != "" {
			if it, err := s.st.FindLibraryItem(store.KindSkill, v.Skill); err == nil && it.Status == store.ItemPublished {
				v.SkillItemID = it.ID
			}
		}
		if r.TaskID != nil {
			for _, t := range tasks {
				if t.ID == *r.TaskID {
					v.Ref = t.Ref()
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// getAccess cauta cererea; lipsa se raporteaza ca 404.
func (s *Server) getAccess(r *http.Request) (store.AccessRequest, error) {
	id, err := pathID(r)
	if err != nil {
		return store.AccessRequest{}, err
	}
	req, err := s.st.GetAccessRequest(id)
	if errors.Is(err, sql.ErrNoRows) {
		return req, store.ErrNotFound
	}
	return req, err
}

func (s *Server) dismissAccess(w http.ResponseWriter, r *http.Request) {
	req, err := s.getAccess(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.st.DismissAccessRequest(req.ID); err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(req.ProjectID, "access", map[string]int64{"agentId": req.AgentID})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) enableShell(w http.ResponseWriter, r *http.Request) {
	s.enableTool(w, r, "shell")
}

func (s *Server) enablePackages(w http.ResponseWriter, r *http.Request) {
	s.enableTool(w, r, "packages")
}

// enableTool porneste tool-ul ("shell" sau "packages") prin team, care trezeste agentul.
func (s *Server) enableTool(w http.ResponseWriter, r *http.Request, tool string) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.team.EnableTool(id, tool); err != nil {
		if errors.Is(err, team.ErrNotToolRequest) || errors.Is(err, team.ErrRequestClosed) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// installSkillAccess: POST /api/access/{id}/install-skill — skill-ul refuzat, deja in Library, se instaleaza
// (versiunea cea mai noua) si se activeaza in proiectul cererii; team.SkillAvailable inchide cardurile si trezeste agentul.
func (s *Server) installSkillAccess(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	req, err := s.getAccess(r)
	if err != nil {
		fail(w, err)
		return
	}
	name := skillOf(req)
	if req.Dismissed || name == "" {
		writeErr(w, http.StatusConflict, "This request can't be solved by installing a skill.")
		return
	}
	it, err := s.st.FindLibraryItem(store.KindSkill, name)
	if errors.Is(err, store.ErrNotFound) || (err == nil && it.Status != store.ItemPublished) {
		writeErr(w, http.StatusConflict, "\""+name+"\" is not in the Library yet. Add it first.")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	vs, err := s.st.ListLibraryVersions(it.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if len(vs) == 0 {
		writeErr(w, http.StatusConflict, "this item has no published version")
		return
	}
	if err := s.installInProject(req.ProjectID, it.ID, vs[0].ID, nil); err != nil {
		fail(w, err)
		return
	}
	s.hub.Publish(req.ProjectID, "project", map[string]int64{"id": req.ProjectID})
	if err := s.team.SkillAvailable(req.ProjectID, it.Name); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// skillInstalled: dupa o instalare din Library intr-un proiect; un skill inchide cardurile lui (erorile doar in log).
func (s *Server) skillInstalled(pid int64, it store.LibraryItem) {
	if it.Kind != store.KindSkill {
		return
	}
	if err := s.team.SkillAvailable(pid, it.Name); err != nil {
		log.Printf("skill %s installed in project %d: %v", it.Name, pid, err)
	}
}
