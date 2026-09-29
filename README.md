# agentdash

Control local pentru agentii Claude Code. Un singur binar, UI in browser pe 127.0.0.1.

## Rulare in dev

    go run ./cmd/agentdash start

Deschide automat http://127.0.0.1:7777. Optiuni: `--port 8080`, `--no-open`.

## Build

    go build -o agentdash ./cmd/agentdash
    GOOS=darwin GOARCH=arm64 go build -o agentdash-mac ./cmd/agentdash

## Agenti

Un agent = un folder in `~/.agentdash/agents/<nume>/`:

    CLAUDE.md               rolul si regulile agentului
    .claude/settings.json   permisiuni (ce tool-uri are voie sa foloseasca)
    .mcp.json               MCP servers (optional)

Claude ruleaza cu folderul agentului ca working directory. La prima pornire se creeaza un agent `demo` read-only.

Important: in modul `-p` nu exista prompt interactiv de permisiuni. Actiunile care nu sunt in `permissions.allow` sunt refuzate (cele read-only, ca Read sau `ls`, sunt permise oricum), deci configureaza settings.json pentru fiecare agent.

Folderele agentilor nu trebuie marcate ca "trusted" in Claude Code: runner-ul trimite explicit `--settings .claude/settings.json` si `--mcp-config .mcp.json` (daca exista), plus `--setting-sources user`, ca sa nu mai citeasca settings-ul de proiect si sa nu mai apara avertismentul de trust.

## Structura

    cmd/agentdash/        CLI: start, oprire curata la Ctrl+C
    internal/config/      caile de pe disc (~/.agentdash)
    internal/agent/       citeste agentii din foldere
    internal/runner/      porneste claude, parseaza stream-json, log + broadcast
    internal/web/         API + SSE, UI embedded in binar

Loguri per run: `~/.agentdash/logs/runs/<id>.jsonl`.

## Stare MVP

Gata: start, lista agenti, run cu output live, chat continuu (--resume), stop (omoara si procesele copil).
Urmeaza: SQLite pentru istoric si sesiuni persistente dupa restart, lista run-urilor, cost per run.
