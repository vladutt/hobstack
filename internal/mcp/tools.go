package mcp

// Tool e definitia unui tool asa cum o trimitem la tools/list.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func schema(required []string, props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func str(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

var leadTools = []Tool{
	{
		Name:        "create_task",
		Description: "Creeaza un task nou in Queued si il asigneaza unui worker din echipa. Worker-ul il primeste automat.",
		InputSchema: schema([]string{"title", "assignee"}, map[string]any{
			"title":       str("Titlu scurt si clar"),
			"description": str("Ce trebuie facut, criterii de acceptare, fisiere relevante"),
			"assignee":    str("Numele worker-ului, exact ca in lista echipei"),
		}),
	},
	{
		Name:        "list_tasks",
		Description: "Lista task-urilor proiectului, cu status, worker si rezultat.",
		InputSchema: schema([]string{}, map[string]any{
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"queued", "in_progress", "review", "done", "blocked"},
				"description": "Optional: doar task-urile cu acest status",
			},
		}),
	},
	{
		Name:        "accept_task",
		Description: "Accepta un task din Review (sau blocat) si il muta in Done.",
		InputSchema: schema([]string{"task"}, map[string]any{
			"task": str("Task-ul, ex. T-3"),
			"note": str("Optional: o nota pentru istoric"),
		}),
	},
	{
		Name:        "send_back",
		Description: "Trimite un task din Review inapoi worker-ului, cu feedback concret. Dupa 3 trimiteri task-ul se blocheaza si decide utilizatorul.",
		InputSchema: schema([]string{"task", "feedback"}, map[string]any{
			"task":     str("Task-ul, ex. T-3"),
			"feedback": str("Ce trebuie schimbat, concret"),
		}),
	},
}

// createTaskInStages: create_task intr-un proiect cu etape (coloane proprii).
var createTaskInStages = Tool{
	Name:        "create_task",
	Description: "Creeaza un task nou. Intra in prima etapa a board-ului si il ia primul agent liber din acea etapa; dupa ultima etapa decizi tu (accept_task / send_back).",
	InputSchema: schema([]string{"title"}, map[string]any{
		"title":       str("Titlu scurt si clar"),
		"description": str("Ce trebuie facut, criterii de acceptare, fisiere relevante"),
		"assignee":    str("Optional: un agent din prima etapa care sa-l ia primul; gol = primul agent liber"),
	}),
}

var reportTool = Tool{
	Name:        "report_done",
	Description: "Raporteaza sefului ca ai terminat un task (sau ca esti blocat). Task-ul trece in Review, la reviewer-ul tau daca ai unul.",
	InputSchema: schema([]string{"summary"}, map[string]any{
		"task":    str("Task-ul raportat, ex. T-3. Intr-un run pe task e implicit task-ul curent; altfel e obligatoriu."),
		"summary": str("Rezumatul cerut in definition of done, sau problema care te blocheaza"),
	}),
}

var reviewTools = []Tool{
	{
		Name:        "review_approve",
		Description: "Aproba munca unui agent pe care il verifici. Task-ul trece la liderul care l-a creat, pentru decizia finala.",
		InputSchema: schema([]string{"task"}, map[string]any{
			"task": str("Task-ul verificat, ex. T-3"),
			"note": str("Optional: o nota pentru lider"),
		}),
	},
	{
		Name:        "review_reject",
		Description: "Respinge munca unui agent pe care il verifici, cu feedback concret. Task-ul revine agentului. Dupa 3 respingeri sau trimiteri inapoi, task-ul se blocheaza si decide utilizatorul.",
		InputSchema: schema([]string{"task", "feedback"}, map[string]any{
			"task":     str("Task-ul verificat, ex. T-3"),
			"feedback": str("Ce e gresit si ce trebuie schimbat, concret"),
		}),
	},
}

var proposeSetupTool = Tool{
	Name:        "propose_setup",
	Description: "Trimite propunerea de proiect si echipa. Apeleaza-l o singura data, la final. Daca primesti o eroare, corecteaza propunerea si apeleaza din nou.",
	InputSchema: schema([]string{"name", "goal", "context", "agents"}, map[string]any{
		"name":      str("Numele proiectului, scurt"),
		"goal":      str("Obiectivul: cum arata gata"),
		"context":   str("Context comun pentru toti agentii: stack, comenzi de build/test/rulare, conventii, structura"),
		"rationale": str("Pe scurt: de ce aceasta echipa"),
		"agents": map[string]any{
			"type":        "array",
			"description": "2-6 agenti (maxim 12). Cel putin unul cu reportsTo gol (raporteaza utilizatorului).",
			"items": schema([]string{"name", "role", "responsibilities", "reportsTo"}, map[string]any{
				"name":             str("Nume scurt, unic, ex. Rook"),
				"role":             str("Rolul, ex. Backend Developer"),
				"model":            map[string]any{"type": "string", "enum": []string{"opus", "sonnet", "haiku"}, "description": "Modelul Claude"},
				"responsibilities": str("Ce ia: tipurile de task-uri pe care le primeste"),
				"reportsTo":        str("Numele sefului din aceasta propunere; gol = raporteaza utilizatorului"),
				"reviewedBy":       str("Optional: numele celui care ii verifica munca"),
				"background":       str("Ce stie despre acest proiect"),
				"instructions":     str("Cum lucreaza si ce nu face niciodata"),
				"doneDef":          str("Ce preda cand termina"),
				"tools": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string", "enum": []string{"write", "git"}},
				},
			}),
		},
	}),
}

// toolsFor intoarce reuniunea tool-urilor pentru rolurile agentului.
// Un agent radacina fara copii nu primeste niciun tool.
func toolsFor(c Caller) []Tool {
	if c.SetupID != "" {
		return []Tool{proposeSetupTool} // analistul din wizard nu are tool-uri de echipa
	}
	out := []Tool{}
	if c.IsLead {
		out = append(out, leadTools...)
		if c.HasStages {
			out[0] = createTaskInStages
		}
	}
	if c.HasParent {
		out = append(out, reportTool)
	}
	if c.IsReviewer {
		out = append(out, reviewTools...)
	}
	// un run pe un task dintr-o etapa primeste tool-urile etapei, fara dubluri
	has := func(name string) bool {
		for _, t := range out {
			if t.Name == name {
				return true
			}
		}
		return false
	}
	if c.StageKind != "" && !has(reportTool.Name) {
		out = append(out, reportTool)
	}
	if c.StageKind == "check" && !has("review_approve") {
		out = append(out, reviewTools...)
	}
	return out
}

func allowed(c Caller, name string) bool {
	for _, t := range toolsFor(c) {
		if t.Name == name {
			return true
		}
	}
	return false
}
