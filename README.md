# Foreman

Echipe de agenti Claude Code, local. Un singur binar Go, UI in browser pe 127.0.0.1.

Fiecare proiect are o echipa: un **lead** care primeste obiectivul, il sparge in task-uri
si le da **workerilor**, apoi verifica ce vine inapoi. Tu vorbesti cu lead-ul sau direct
cu orice agent. Totul se vede live pe un board si ramane salvat in SQLite.

## Rulare

    go run ./cmd/foreman start

Deschide http://127.0.0.1:7777. Optiuni: `--port 8080`, `--no-open`.
Ai nevoie de `claude` (Claude Code) in PATH, autentificat.

## Build

    go build -o foreman ./cmd/foreman
    GOOS=darwin GOARCH=arm64 go build -o foreman-mac ./cmd/foreman

## Cum functioneaza

- Agentii ruleaza ca `claude -p` headless, cu folderul proiectului ca working directory.
- Aplicatia expune un server MCP pe `/mcp/<token>`. Lead-ul are tool-urile `create_task`,
  `list_tasks`, `accept_task` si `send_back`, iar workerii au `report_done`.
- Cand un worker termina, lead-ul e trezit (`--resume`) cu rezultatul.
- Permisiunile vin doar din aplicatie (`--settings` inline, `--setting-sources user`). `CLAUDE.md`
  din repo-ul proiectului se citeste in continuare. `git push`, `rm -rf`, `sudo` si instalarile
  de pachete sunt mereu blocate.
- La pornire toate proiectele sunt pe pauza: nimic nu ruleaza (si nu costa) fara sa apesi „Porneste echipa”.

## Date

    ~/.foreman/foreman.db           proiecte, agenti, task-uri, run-uri, mesaje
    ~/.foreman/logs/runs/<id>.jsonl logul brut al fiecarui run

## Structura

    cmd/foreman/        CLI: start, oprire curata la Ctrl+C
    internal/config/    AppName si caile de pe disc
    internal/store/     SQLite (modernc.org/sqlite, fara CGO)
    internal/prompt/    system prompt, mesajul turei, permisiuni, config MCP
    internal/runner/    porneste claude, parseaza stream-json, log + broadcast
    internal/mcp/       server MCP minimal (JSON-RPC peste HTTP)
    internal/team/      scheduler-ul: tool-urile echipei, pornire/final de run-uri
    internal/events/    evenimente live per proiect (SSE)
    internal/web/       API, SSE, UI embedded

## Redenumire

Numele e in `internal/config/config.go` (`AppName`). Il schimbi acolo, redenumesti
`cmd/<nume>`, rulezi `go mod edit -module <nume>` si faci replace pe importuri.
