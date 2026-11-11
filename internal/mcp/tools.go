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

// continuesArg: argumentul optional continues al lui create_task (fir de lucru, doar afisare)
const continuesArg = "Optional: the task this continues (e.g. T-85), so it is grouped with it on the board"

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
			"continues":   str(continuesArg),
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
		Description: "Sends a task in Review back to the worker, with concrete feedback. After 3 send-backs the task becomes blocked and goes to the user (or, in full autonomy, to the root lead).",
		InputSchema: schema([]string{"task", "feedback"}, map[string]any{
			"task":     str("The task, e.g. T-3"),
			"feedback": str("What needs to change, concretely"),
		}),
	},
}

// createTaskInStages: create_task intr-un proiect cu etape (coloane proprii). Etapele sunt optionale:
// fara stage task-ul merge la un agent din echipa directa, ca fara etape.
var createTaskInStages = Tool{
	Name: "create_task",
	Description: "Creates a new task. Without stage it goes to a worker on your direct team (assignee required) and moves through Queued, In progress and Review as usual. " +
		"With stage it enters that board stage: the first free agent in the stage takes it, it moves through the next stages, and after the last stage you decide (accept_task / send_back).",
	InputSchema: schema([]string{"title"}, map[string]any{
		"title":       str("Short, clear title"),
		"description": str("What needs to be done, acceptance criteria, relevant files"),
		"assignee":    str("The worker's name. Required without stage; with stage optional (an agent from that stage to take it first; empty = the first free agent)"),
		"stage":       str("Optional: the board stage the task enters, by name, exactly as in the stages list"),
		"continues":   str(continuesArg),
	}),
}

// startTaskTool: in run-urile fara task (chat), agentul isi porneste un task pentru o modificare ceruta.
var startTaskTool = Tool{
	Name:        "start_task",
	Description: "Starts a task assigned to you for a change the user asked for in this conversation. Changes are made only in tasks: call it, then stop; the task starts right after this run with the same conversation.",
	InputSchema: schema([]string{"title"}, map[string]any{
		"title":       str("Short, clear title"),
		"description": str("What needs to be done, acceptance criteria, relevant files"),
		"continues":   str(continuesArg),
	}),
}

var reportTool = Tool{
	Name:        "report_done",
	Description: "Reports to your boss that you finished a task (or that you are blocked). The task moves to Review, to your reviewer if you have one.",
	InputSchema: schema([]string{"summary"}, map[string]any{
		"task":    str("The task being reported, e.g. T-3. In a task run it defaults to the current task; otherwise it is required."),
		"summary": str("The summary required by the definition of done, or the problem blocking you"),
		"left_for_user": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Optional: up to 10 values only the user can give (e.g. Company registration number), which you left as placeholders. The user answers in the task's comments.",
		},
		"decisions": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question": str("The question for the user (max 300 characters)"),
					"options":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "2 to 5 short choices (max 120 characters each)"},
					"multi":    map[string]any{"type": "boolean", "description": "true if the user may pick several options (default false)"},
				},
				"required": []string{"question", "options"},
			},
			"description": "Optional: up to 5 questions the user answers by picking an option (e.g. whether to open a follow-up task). Use these instead of asking in the summary.",
		},
		"user_steps": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Optional: up to 10 things only a person can do and you could not (a visual check, signing in to an account, a physical device). Never commands, tests or checks you can run yourself, and not a guide to try the work. They appear as a checklist for the user.",
		},
		"files": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Optional: up to 20 files you produced as the deliverable of this task (documents, images, exports), relative to your folder (or absolute, e.g. in the project files folder). They are copied and attached to the task. Do not list code you changed; it is already shown as changes.",
		},
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
		Description: "Rejects the work of an agent you review, with concrete feedback. The task goes back to the agent. After 3 rejections or send-backs, the task becomes blocked and goes to the user (or, in full autonomy, to the root lead).",
		InputSchema: schema([]string{"task", "feedback"}, map[string]any{
			"task":     str("The reviewed task, e.g. T-3"),
			"feedback": str("What is wrong and what needs to change, concretely"),
		}),
	},
}

var proposeAgentTool = Tool{
	Name:        "propose_agent",
	Description: "Submits the proposed agent. Call it once, at the end. If you get an error, fix the proposal and call it again.",
	InputSchema: schema([]string{"name", "role", "responsibilities"}, map[string]any{
		"name":             str("Short, unique name, e.g. Herald"),
		"role":             str("The role, e.g. E-mail & Notifications Developer"),
		"responsibilities": str("Takes: what it is responsible for, and what it does not take"),
		"model":            map[string]any{"type": "string", "enum": []string{"opus", "sonnet", "haiku"}, "description": "The Claude model"},
		"reportsTo":        str("The name of its boss in the team; empty = reports to the user"),
		"reviewedBy":       str("Optional: the name of the agent who reviews its work"),
		"tools": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string", "enum": []string{"write", "shell", "packages", "git", "web"}},
		},
		"instructions": str("How it works and what it never does"),
		"background":   str("What it knows about this project"),
		"doneDef":      str("What it hands over when it finishes"),
	}),
}

var brainstormReplyTool = Tool{
	Name:        "brainstorm_reply",
	Description: "Submits your answer to the brainstorming session: one idea or one question. Call it exactly once per run. Use kind \"wild\" only for truly bold ideas. Use kind \"question\" only when the answer would change your idea.",
	InputSchema: schema([]string{"kind", "title", "text"}, map[string]any{
		"kind":  map[string]any{"type": "string", "enum": []string{"idea", "wild", "question"}, "description": "idea, wild (a bold idea) or question"},
		"title": str("Short title, max 120 characters"),
		"text":  str("The idea or the question"),
		"basis": str("What you base it on: files, attachments, what you found in the project"),
		"options": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Only for a question: 2-5 answer options",
		},
	}),
}

var proposeSetupTool = Tool{
	Name:        "propose_setup",
	Description: "Submits the project and team proposal. Call it once, at the end. If you get an error, fix the proposal and call it again.",
	InputSchema: schema([]string{"name", "goal", "context", "agents"}, map[string]any{
		"name":      str("The project name, short"),
		"goal":      str("The goal: what done looks like"),
		"context":   str("Shared context for all agents: stack, build/test/run commands, conventions, structure"),
		"rationale": str("Briefly: why this team"),
		"folders": map[string]any{
			"type":        "array",
			"description": "Only with several folders: exactly the given folders, each with a short name and its own context. With one folder you may omit it.",
			"items": schema([]string{"path", "name"}, map[string]any{
				"path":    str("The folder path, exactly as given"),
				"name":    str("Short name, e.g. Client or Landing"),
				"context": str("Context for this folder: stack, build/test/run commands, conventions"),
			}),
		},
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
				"folder":           str("The agent's folder (a name from folders); empty = the first folder"),
				"tools": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string", "enum": []string{"write", "shell", "git"}},
				},
			}),
		},
	}),
}

var tickLeftTool = Tool{
	Name:        "tick_left_for_user",
	Description: "Ticks off items you left for the user (left_for_user) once you have filled them in.",
	InputSchema: schema([]string{"task", "items"}, map[string]any{
		"task":  str("The task, e.g. T-124"),
		"items": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "The item numbers, e.g. [1, 3]"},
	}),
}

var installMCPTool = Tool{
	Name: "install_mcp",
	Description: "Asks the user to install an MCP server in this project, so the project's agents get its tools " +
		"(for example a browser, a database or an API). Normally only the user can install it: the request appears in their " +
		"Needs you list with the exact command, and you are told when it is installed or declined. If you are a lead or " +
		"report to the user and the user asked you in chat to install it, it is installed right away (after a quick test), " +
		"unless it needs keys. Its tools are available from your next run. Prefer servers from the official MCP registry " +
		"and their documented command.",
	InputSchema: schema([]string{"name", "description", "reason"}, map[string]any{
		"name":        str("Short name: lowercase letters, digits and dashes, e.g. playwright"),
		"description": str("What the server does, in one line (max 300 characters)"),
		"command":     str("Local server: the program to start, e.g. npx (leave empty for a remote server)"),
		"args":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Local server: its arguments, e.g. [\"-y\", \"@playwright/mcp@latest\"]"},
		"url":         str("Remote server: its http(s) address (leave empty for a local server)"),
		"secrets":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Names of the keys it needs, e.g. [\"GITHUB_TOKEN\"]; the user fills in the values (a remote server can only have AUTH_TOKEN)"},
		"reason":      str("Why you need it, for the user (max 300 characters)"),
	}),
}

var attachFilesTool = Tool{
	Name: "attach_files",
	Description: "Shows the user files you made, so they can open and check them: screenshots, images, SVGs, PDFs, " +
		"exports, generated pages. On a task they go to the task's Files; in a chat they appear in your message. " +
		"Use it whenever you build or check something visual (for example after a screenshot from a test). " +
		"Images, SVGs and PDFs you create on a task are also attached automatically when your run ends.",
	InputSchema: schema([]string{"paths"}, map[string]any{
		"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
			"description": "Up to 20 files, relative to your folder (or absolute inside the project folders), e.g. [\"test-results/checkout.png\"]"},
		"note": str("Optional, chat only: a short line shown with the files, e.g. \"The checkout page on mobile\""),
	}),
}

var deleteFilesTool = Tool{
	Name: "delete_files",
	Description: "Deletes files or folders that you created yourself (for example scratch files, a temporary folder, " +
		"build output you made), without asking the user. Use it instead of rm. It refuses anything you did not create, " +
		"anything outside your folder and anything committed to git; for those, leave them or ask your lead.",
	InputSchema: schema([]string{"paths"}, map[string]any{
		"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
			"description": "Paths relative to your folder (or absolute inside it), e.g. [\".scratch\", \"out/test.log\"]"},
	}),
}

var commentTaskTool = Tool{
	Name:        "comment_task",
	Description: "Answers the user in a task's comments (for example after they gave you a value you left for them).",
	InputSchema: schema([]string{"task", "text"}, map[string]any{
		"task": str("The task, e.g. T-124"),
		"text": str("Your reply, short"),
	}),
}

var askUserTool = Tool{
	Name:        "ask_user",
	Description: "Ask the user a question when you need a decision or information only they can give (not for denied commands: those reach the user on their own). After calling it, stop: the answer arrives later as a message. Do not call report_done while you wait.",
	InputSchema: schema([]string{"question"}, map[string]any{
		"question": str("The question, clear and self-contained (what you need and why)"),
		"options": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Optional: 2-4 short answers the user can pick with one click",
		},
	}),
}

var addAgentTools = []Tool{
	{
		Name: "add_agent",
		Description: "Adds a new agent to your direct team. Use it only when no current member fits part of the work, or to run " +
			"independent sub-tasks in parallel. Depending on the project, the agent is added at once or the user approves it first; " +
			"the result tells you which. A new agent starts working when you give it a task with create_task.",
		InputSchema: schema([]string{"name", "role", "instructions", "reason"}, map[string]any{
			"name":            str("Short, unique name, e.g. Quinn"),
			"role":            str("The role, e.g. QA Engineer"),
			"instructions":    str("How it works and what it never does"),
			"background":      str("Optional: what it knows about this project"),
			"done_definition": str("Optional: what it hands over when it finishes"),
			"model":           map[string]any{"type": "string", "enum": []string{"opus", "sonnet", "haiku"}, "description": "Optional: the Claude model (default sonnet)"},
			"tools": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "enum": []string{"write", "shell", "packages", "git", "web"}},
				"description": "Optional: only tools you have yourself (default write)",
			},
			"reason": str("One sentence: why the team needs this agent (the user sees it)"),
		}),
	},
	{
		Name:        "remove_agent",
		Description: "Removes from your team an agent you added, once it has no open tasks. It stays on the canvas, detached, with its history.",
		InputSchema: schema([]string{"name"}, map[string]any{
			"name": str("The agent's name"),
		}),
	},
}

// scheduleArg: programul unei sarcini recurente (ora locala a masinii)
var scheduleArg = map[string]any{
	"type":        "object",
	"description": "When it runs (local time). every_hours: every N hours; daily: every day at `at`; weekly: on `weekdays` at `at`",
	"properties": map[string]any{
		"kind":        map[string]any{"type": "string", "enum": []string{"every_hours", "daily", "weekly"}},
		"every_hours": map[string]any{"type": "integer", "description": "every_hours: 1-24"},
		"at":          str("daily / weekly: HH:MM, 24h"),
		"weekdays":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": `weekly: days, e.g. ["mon", "wed", "fri"]`},
	},
	"required": []string{"kind"},
}

var recurringIDArg = str("The recurring task, e.g. R-3 (from list_recurring)")

// recurringTools: liderii si radacinile; schimbarile doar la cererea utilizatorului din chat (verificat la apel)
var recurringTools = []Tool{
	{
		Name:        "list_recurring",
		Description: "Lists this project's recurring tasks (R-N): schedule, who does them, on or paused, next run.",
		InputSchema: schema([]string{}, map[string]any{}),
	},
	{
		Name: "create_recurring",
		Description: "Creates a recurring task: it puts a task on the board on a schedule, done by you or an agent of your direct team. " +
			"Use it ONLY when the user asks for it in this chat; otherwise suggest it in your reply. The user decides each run " +
			"(or it is accepted automatically with auto_accept). Check list_recurring first so you do not create a duplicate.",
		InputSchema: schema([]string{"title", "schedule"}, map[string]any{
			"title":       str("Short, clear title"),
			"description": str("What to do on each run and what to report"),
			"assignee":    str(`Who does it: you, an agent of your direct team, or "first stage" (root agent, projects with stages). Default: you`),
			"schedule":    scheduleArg,
			"auto_accept": map[string]any{"type": "boolean", "description": "Accept each run automatically when it finishes (default false)"},
		}),
	},
	{
		Name: "update_recurring",
		Description: "Changes a recurring task when the user asks for it in this chat. Pass only what changes; " +
			"enabled false pauses it, true resumes it.",
		InputSchema: schema([]string{"id"}, map[string]any{
			"id":          recurringIDArg,
			"title":       str("New title"),
			"description": str("New description"),
			"assignee":    str(`Who does it: you, an agent of your direct team, or "first stage"`),
			"schedule":    scheduleArg,
			"auto_accept": map[string]any{"type": "boolean"},
			"enabled":     map[string]any{"type": "boolean", "description": "false = pause, true = resume"},
		}),
	},
	{
		Name:        "delete_recurring",
		Description: "Deletes a recurring task when the user asks for it in this chat. Tasks it already created stay on the board.",
		InputSchema: schema([]string{"id"}, map[string]any{"id": recurringIDArg}),
	},
}

var linkedProjectsTool = Tool{
	Name:        "list_linked_projects",
	Description: "Lists the projects linked with yours: name, goal and root agent. Questions and work requests go to their root agent.",
	InputSchema: schema([]string{}, map[string]any{}),
}

var askProjectTool = Tool{
	Name:        "ask_project",
	Description: "Asks a linked project a question; its root agent answers. The answer arrives later as a message, so continue meanwhile. Only ask what your own team cannot answer.",
	InputSchema: schema([]string{"project", "question"}, map[string]any{
		"project":  str("The linked project's name, exactly as in list_linked_projects"),
		"question": str("The question, clear and self-contained"),
	}),
}

var replyToProjectTool = Tool{
	Name:        "reply_to_project",
	Description: "Answers a question a linked project asked your project (it arrives as a message with a reference like Q-7).",
	InputSchema: schema([]string{"request", "answer"}, map[string]any{
		"request": str("The question's reference, e.g. Q-7"),
		"answer":  str("Your answer, at most 3800 characters"),
	}),
}

var requestWorkTool = Tool{
	Name:        "request_project_work",
	Description: "Asks a linked project to do work for you. It becomes a task for its root agent (usually after the user approves it); its report comes back to you as a message.",
	InputSchema: schema([]string{"project", "title"}, map[string]any{
		"project":     str("The linked project's name, exactly as in list_linked_projects"),
		"title":       str("Short, clear title"),
		"description": str("What is needed, acceptance criteria, how you will use it"),
	}),
}

var proposeLibraryTool = Tool{
	Name: "propose_library_item",
	Description: "Proposes something you built here for the shared Library, so other projects can use it: a script (tool), " +
		"instructions (skill, a folder with SKILL.md) or an MCP server. The user reviews and publishes it; keep using it in this project meanwhile.",
	InputSchema: schema([]string{"kind", "name", "description", "files"}, map[string]any{
		"kind":        map[string]any{"type": "string", "enum": []string{"tool", "skill", "mcp"}},
		"name":        str("lowercase-with-dashes, e.g. pdf-offer"),
		"description": str("One sentence: what it does"),
		"files":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Paths relative to your folder (tool / mcp: max 20 files, 1 MB; skill: max 200 files, 10 MB)"},
		"entry":       str("tool: the file to run, e.g. pdf-offer.sh"),
		"command":     str("mcp (local): the command that starts the server, e.g. python3"),
		"args":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "mcp (local): its arguments; ${DIR} is the server's folder"},
		"url":         str("mcp (remote): the server URL"),
		"needs":       map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"network", "write", "browser", "shell"}}},
		"secrets":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Names of secrets each project provides, e.g. META_TOKEN (not PATH, HOME, LD_*, DYLD_* and similar). A remote mcp can only have AUTH_TOKEN (sent as a Bearer token). A tool with secrets cannot use run as its entry."},
	}),
}

// toolsFor intoarce reuniunea tool-urilor pentru rolurile agentului.
// Orice agent al echipei primeste ask_user; analistul din wizard nu.
func toolsFor(c Caller) []Tool {
	if c.BrainstormCardID != 0 {
		return []Tool{brainstormReplyTool}
	}
	if c.DraftID != "" {
		return []Tool{proposeAgentTool}
	}
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
	if c.CanAddAgents {
		// o radacina fara echipa primeste create_task / list_tasks ca sa-i dea de lucru agentului nou in acelasi run
		if !c.IsLead {
			out = append(out, leadTools[0], leadTools[1])
		}
		out = append(out, addAgentTools...)
		out = append(out, recurringTools...)
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
	// proiecte legate: intrebari pentru toti, raspunsuri pentru radacini, cereri de lucru pentru lideri
	if c.HasLinks {
		out = append(out, linkedProjectsTool, askProjectTool)
		if c.IsRoot {
			out = append(out, replyToProjectTool)
		}
		if c.CanAddAgents {
			out = append(out, requestWorkTool)
		}
	}
	// radacina livreaza cu report_done munca ceruta de un proiect legat
	if c.FromRequest && !has(reportTool.Name) {
		out = append(out, reportTool)
	}
	// cine poate raporta poate si lasa / bifa puncte pentru utilizator si raspunde in comentarii
	if has(reportTool.Name) {
		out = append(out, tickLeftTool, commentTaskTool)
	}
	if c.CanWrite {
		out = append(out, proposeLibraryTool)
		// pe un task, agentul isi poate sterge singur fisierele create de el
		if c.TaskID != 0 {
			out = append(out, deleteFilesTool)
		}
	}
	// run fara task (chat, notificari): modificarile se fac doar pe task, pornit de agent
	if c.TaskID == 0 {
		out = append(out, startTaskTool)
	}
	out = append(out, askUserTool, attachFilesTool, installMCPTool)
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
