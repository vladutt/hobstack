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

var workerTools = []Tool{
	{
		Name:        "report_done",
		Description: "Raporteaza lead-ului ca ai terminat task-ul curent (sau ca esti blocat). Task-ul trece in Review.",
		InputSchema: schema([]string{"summary"}, map[string]any{
			"summary": str("Rezumatul cerut in definition of done, sau problema care te blocheaza"),
		}),
	},
}

func toolsFor(c Caller) []Tool {
	if c.Lead {
		return leadTools
	}
	return workerTools
}

func allowed(c Caller, name string) bool {
	for _, t := range toolsFor(c) {
		if t.Name == name {
			return true
		}
	}
	return false
}
