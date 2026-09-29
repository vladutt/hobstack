# agentdash

Tool local, personal (NU SaaS), pentru controlul agentilor Claude Code.
Un singur binar Go instalat pe masina; UI in browser pe 127.0.0.1:7777.
Autorul e senior PHP/Laravel, incepator in Go: cod simplu, idiomatic, comentat unde nu e evident.

## Decizii luate
- Go in loc de Laravel/FrankenPHP: binar unic, daemon natural, goroutines pentru streaming.
- Claude Code CLI headless (`claude -p --output-format stream-json --verbose --resume <sid>`), nu Agent SDK.
- Un agent = un folder in ~/.agentdash/agents/<nume>/ (CLAUDE.md, .claude/settings.json, .mcp.json). Claude ruleaza cu acel folder ca cwd.
- Live output prin SSE (nu websockets). UI: HTML + JS vanilla, embedded cu `embed`.
- Fara Docker la runtime; Docker doar pentru dev daca e nevoie.
- Un singur run activ per agent. Stop omoara tot process group-ul.
- Asculta doar pe localhost, fara autentificare.
- Preferinta: dependente minime. SQLite planificat prin modernc.org/sqlite (pure Go, fara CGO).

## MVP
Gata: start, lista agenti, run cu output live, chat continuu (--resume), stop.
Urmeaza, in ordine:
1. SQLite: sesiuni per agent si run-uri persistente dupa restart
2. Istoric run-uri per agent (status, durata, cost din evenimentul `result`), redeschidere log
Dupa MVP: cron-uri (robfig/cron/v3), editare agenti din UI, `service install` (launchd/systemd), goreleaser, notificari, git worktrees.

## Conventii
- `go vet ./...` si `go build -race` trebuie sa treaca.
- Verifica cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/agentdash` si `GOOS=windows go build ./cmd/agentdash`.
- Mesajele pentru utilizator in romana, fara diacritice.
