package store

// Rolurile unui agent nu se stocheaza: reies din legaturi.
type Roles struct {
	IsLead     bool `json:"isLead"`     // are cel putin un agent direct sub el
	IsRoot     bool `json:"isRoot"`     // raporteaza utilizatorului (fara sef si legat de el)
	IsReviewer bool `json:"isReviewer"` // verifica munca cel putin unui agent
}

func RolesOf(a Agent, team []Agent) Roles {
	r := Roles{IsRoot: a.ParentID == nil && !a.Detached}
	for _, o := range team {
		if o.ParentID != nil && *o.ParentID == a.ID {
			r.IsLead = true
		}
		if o.ReviewerID != nil && *o.ReviewerID == a.ID {
			r.IsReviewer = true
		}
	}
	return r
}

// Children intoarce agentii aflati direct sub id, in ordinea din team.
func Children(id int64, team []Agent) []Agent {
	var out []Agent
	for _, o := range team {
		if o.ParentID != nil && *o.ParentID == id {
			out = append(out, o)
		}
	}
	return out
}

// FindAgent cauta un agent dupa id in lista data.
func FindAgent(team []Agent, id int64) (Agent, bool) {
	for _, a := range team {
		if a.ID == id {
			return a, true
		}
	}
	return Agent{}, false
}

// IsAncestor spune daca ancestorID e undeva deasupra lui id, pe lantul de
// sefi. Un agent nu e propriul stramos.
func IsAncestor(ancestorID, id int64, team []Agent) bool {
	cur, ok := FindAgent(team, id)
	// limita de pasi: protectie la cicluri in date stricate
	for steps := 0; ok && cur.ParentID != nil && steps < len(team); steps++ {
		if *cur.ParentID == ancestorID {
			return true
		}
		cur, ok = FindAgent(team, *cur.ParentID)
	}
	return false
}
