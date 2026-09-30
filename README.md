# Foreman

Local Claude Code agent teams. A single Go binary, with a browser UI on 127.0.0.1.

A project's team is a hierarchy: you give goals to the root agents, leads split the work
only among their direct team, and work goes through the reviewer (if there is one), then to the lead
that created the task. You can talk to any agent. Everything is visible live on a board and saved in SQLite.

## Running

    go run ./cmd/foreman start

Open http://127.0.0.1:7777. Options: `--port 8080`, `--no-open`.
The project's working folder is optional: if you don't choose one, `~/Foreman/<name>` is used.
You need `claude` (Claude Code) in PATH, logged in.

## Build

    go build -o foreman ./cmd/foreman
    GOOS=darwin GOARCH=arm64 go build -o foreman-mac ./cmd/foreman

## How it works

- Agents run as headless `claude -p`, with the project folder as the working directory.
- The app exposes an MCP server at `/mcp/<token>`. The tools depend on the role:
  - leads have `create_task`, `list_tasks`, `accept_task` and `send_back`;
  - agents with a boss have `report_done`;
  - reviewers have `review_approve` and `review_reject`;
  - a root agent without a team has no tools.
- When an agent finishes, the reviewer (if any) and then the lead that created the task are
  woken up (`--resume`) with the result.
- A sub-lead splits the task it received into subtasks for its team. When all subtasks
  are done, the task returns to its queue, and the sub-lead checks it and reports it up.
- On the canvas, nodes are moved with the mouse, links are dragged from ●, and double-click opens the editor.
- In the API (`POST /api/projects/{id}/agents`, `PUT /api/agents/{id}`), a missing or null `parentId` means
  a root agent.
- `max_parallel` applies only to agents that have a boss.
- Permissions come only from the app (`--settings` inline, `--setting-sources user`). `CLAUDE.md`
  from the project's repo is still read. `git push`, `rm -rf`, `sudo` and package installs
  are blocked as command prefixes; with "Shell commands" enabled, an agent can get
  around them (`rm -fr`, `bash -c ...`). Only give shell to agents you trust (approvals
  come in a future version).
- Your global Claude settings (`~/.claude/settings.json`) also apply to agents, so a global
  `permissions.allow` rule widens their permissions.
- The API accepts only Host 127.0.0.1/localhost and requires `Content-Type: application/json` on
  requests that change something (protection against web pages that would send requests
  to the app).
- Files attached to a project live in `~/.foreman/projects/<id>/files/`; agents get them with
  `--add-dir` and read them with Read (including images and PDFs).
- On startup all projects are paused: nothing runs (or costs money) until you press "Start team".

## Data

    ~/.foreman/foreman.db           projects, agents, tasks, runs, messages
    ~/.foreman/logs/runs/<id>.jsonl raw log of each run
    ~/.foreman/projects/<id>/files/ files attached to the project

## Structure

    cmd/foreman/        CLI: start, clean shutdown on Ctrl+C
    internal/config/    AppName and paths on disk
    internal/store/     SQLite (modernc.org/sqlite, no CGO)
    internal/prompt/    system prompt, turn message, permissions, MCP config
    internal/runner/    starts claude, parses stream-json, log + broadcast
    internal/mcp/       minimal MCP server (JSON-RPC over HTTP)
    internal/team/      the scheduler: team tools, run start/finish
    internal/events/    live per-project events (SSE)
    internal/web/       API, SSE, embedded UI

## Renaming

The name is in `internal/config/config.go` (`AppName`). Change it there, rename
`cmd/<name>`, run `go mod edit -module <name>` and replace the imports.
