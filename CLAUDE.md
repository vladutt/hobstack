# foreman (fost agentdash)

Tool local, personal (NU SaaS), pentru controlul agentilor Claude Code.
Un singur binar Go instalat pe masina; UI in browser pe 127.0.0.1:7777.
Autorul e senior PHP/Laravel, incepator in Go: cod simplu, idiomatic, comentat unde nu e evident.

## Decizii luate
- Go in loc de Laravel/FrankenPHP: binar unic, daemon natural, goroutines pentru streaming.
- Claude Code CLI headless (`claude -p --output-format stream-json --verbose --resume <sid>`), nu Agent SDK.
- Agentii traiesc in SQLite (~/.foreman/foreman.db), per proiect: un lead + workeri. Toti ruleaza cu folderul proiectului ca cwd.
- Orchestrare prin MCP: aplicatia e server MCP (/mcp/<token>), lead-ul creeaza task-uri, workerii raporteaza. Scheduler in internal/team.
- Permisiuni doar din aplicatie: --settings inline + --setting-sources user; deny fix pe actiuni riscante si pe TaskCreate/TodoWrite/Agent.
- Numele aplicatiei e config.AppName ("foreman"), usor de schimbat.
- Live output prin SSE (nu websockets). UI: HTML + JS vanilla, embedded cu `embed`.
- Fara Docker la runtime; Docker doar pentru dev daca e nevoie.
- Un singur run activ per agent. Stop omoara tot process group-ul.
- Asculta doar pe localhost, fara autentificare.
- Preferinta: dependente minime. SQLite prin modernc.org/sqlite (pure Go, fara CGO).

## Stare
Gata (felia 1, spec in docs/superpowers/specs/2026-09-29-foreman-slice1-design.md): proiecte, echipe lead + workeri,
orchestrare prin MCP, board live, chat per agent, SQLite, pauza la pornire.
Urmeaza (sub-proiecte separate, fiecare cu spec + plan):
2. Librarie de agenti refolositi intre proiecte, template-uri de echipa, reviewer separat
3. Aprobari pentru actiuni riscante (--permission-prompt-tool), feed global "Live activity"
4. Modurile collab/pipeline, Runs & logs, Settings
Dupa: cron-uri, service install, goreleaser, notificari, git worktrees.

## Conventii
- `go vet ./...` si `go build -race` trebuie sa treaca.
- Verifica cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Mesajele pentru utilizator in romana, fara diacritice.
