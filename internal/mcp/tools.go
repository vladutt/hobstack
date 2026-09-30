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
		Description: "Creates a new task in Queued and assigns it to a worker on your team. The worker receives it automatically.",
		InputSchema: schema([]string{"title", "assignee"}, map[string]any{
			"title":       str("Short, clear title"),
			"description": str("What needs to be done, acceptance criteria, relevant files"),
			"assignee":    str("The worker's name, exactly as in the team list"),
		}),
	},
	{
		Name:        "list_tasks",
		Description: "Lists the project's tasks, with status, worker and result.",
		InputSchema: schema([]string{}, map[string]any{
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"queued", "in_progress", "review", "done", "blocked"},
				"description": "Optional: only tasks with this status",
			},
		}),
	},
	{
		Name:        "accept_task",
		Description: "Accepts a task in Review (or blocked) and moves it to Done.",
		InputSchema: schema([]string{"task"}, map[string]any{
			"task": str("The task, e.g. T-3"),
			"note": str("Optional: a note for the history"),
		}),
	},
	{
		Name:        "send_back",
		Description: "Sends a task in Review back to the worker, with concrete feedback. After 3 send-backs the task becomes blocked and the user decides.",
		InputSchema: schema([]string{"task", "feedback"}, map[string]any{
			"task":     str("The task, e.g. T-3"),
			"feedback": str("What needs to change, concretely"),
		}),
	},
}

// createTaskInStages: create_task intr-un proiect cu etape (coloane proprii).
var createTaskInStages = Tool{
	Name:        "create_task",
	Description: "Creates a new task. It enters the first stage of the board and the first free agent in that stage takes it; after the last stage you decide (accept_task / send_back).",
	InputSchema: schema([]string{"title"}, map[string]any{
		"title":       str("Short, clear title"),
		"description": str("What needs to be done, acceptance criteria, relevant files"),
		"assignee":    str("Optional: an agent from the first stage to take it first; empty = the first free agent"),
	}),
}

var reportTool = Tool{
	Name:        "report_done",
	Description: "Reports to your boss that you finished a task (or that you are blocked). The task moves to Review, to your reviewer if you have one.",
	InputSchema: schema([]string{"summary"}, map[string]any{
		"task":    str("The task being reported, e.g. T-3. In a task run it defaults to the current task; otherwise it is required."),
		"summary": str("The summary required by the definition of done, or the problem blocking you"),
	}),
}

var reviewTools = []Tool{
	{
		Name:        "review_approve",
		Description: "Approves the work of an agent you review. The task goes to the lead who created it, for the final decision.",
		InputSchema: schema([]string{"task"}, map[string]any{
			"task": str("The reviewed task, e.g. T-3"),
			"note": str("Optional: a note for the lead"),
		}),
	},
	{
		Name:        "review_reject",
		Description: "Rejects the work of an agent you review, with concrete feedback. The task goes back to the agent. After 3 rejections or send-backs, the task becomes blocked and the user decides.",
		InputSchema: schema([]string{"task", "feedback"}, map[string]any{
			"task":     str("The reviewed task, e.g. T-3"),
			"feedback": str("What is wrong and what needs to change, concretely"),
		}),
	},
}

var proposeSetupTool = Tool{
	Name:        "propose_setup",
	Description: "Submits the project and team proposal. Call it once, at the end. If you get an error, fix the proposal and call it again.",
	InputSchema: schema([]string{"name", "goal", "context", "agents"}, map[string]any{
		"name":      str("The project name, short"),
		"goal":      str("The goal: what done looks like"),
		"context":   str("Shared context for all agents: stack, build/test/run commands, conventions, structure"),
		"rationale": str("Briefly: why this team"),
		"agents": map[string]any{
			"type":        "array",
			"description": "2-6 agents (at most 12). At least one with an empty reportsTo (reports to the user).",
			"items": schema([]string{"name", "role", "responsibilities", "reportsTo"}, map[string]any{
				"name":             str("Short, unique name, e.g. Rook"),
				"role":             str("The role, e.g. Backend Developer"),
				"model":            map[string]any{"type": "string", "enum": []string{"opus", "sonnet", "haiku"}, "description": "The Claude model"},
				"responsibilities": str("Takes: the kinds of tasks it receives"),
				"reportsTo":        str("The name of the boss in this proposal; empty = reports to the user"),
				"reviewedBy":       str("Optional: the name of the agent who reviews its work"),
				"background":       str("What it knows about this project"),
				"instructions":     str("How it works and what it never does"),
				"doneDef":          str("What it hands over when it finishes"),
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
