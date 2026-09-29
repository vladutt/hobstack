# Foreman

Echipe de agenti Claude Code, local. Un singur binar Go, UI in browser pe 127.0.0.1.

Echipa unui proiect e o ierarhie: tu dai obiective agentilor radacina, liderii impart munca
doar echipei lor directe, iar munca trece pe la reviewer (daca exista), apoi la liderul care a
creat task-ul. Tu vorbesti cu orice agent. Totul se vede live pe un board si ramane salvat in SQLite.

## Rulare

    go run ./cmd/foreman start

Deschide http://127.0.0.1:7777. Optiuni: `--port 8080`, `--no-open`.
Folderul de lucru al proiectului e optional: daca nu alegi unul, se foloseste `~/Foreman/<nume>`.
Ai nevoie de `claude` (Claude Code) in PATH, autentificat.

## Build

    go build -o foreman ./cmd/foreman
    GOOS=darwin GOARCH=arm64 go build -o foreman-mac ./cmd/foreman

## Cum functioneaza

- Agentii ruleaza ca `claude -p` headless, cu folderul proiectului ca working directory.
- Aplicatia expune un server MCP pe `/mcp/<token>`. Tool-urile depind de rol:
  - liderii au `create_task`, `list_tasks`, `accept_task` si `send_back`;
  - agentii cu sef au `report_done`;
  - reviewerii au `review_approve` si `review_reject`;
  - un agent radacina fara echipa nu are tool-uri.
- Cand un agent termina, reviewerul (daca exista) si apoi liderul care a creat task-ul sunt
  trezisi (`--resume`) cu rezultatul.
- Un sub-lider sparge task-ul primit in sub-task-uri pentru echipa lui. Cand toate sub-task-urile
  sunt gata, task-ul revine in coada lui, iar sub-liderul il verifica si il raporteaza in sus.
- Pe canvas nodurile se muta cu mouse-ul, legaturile se trag din ●, iar dublu-click deschide editorul.
- `max_parallel` se aplica doar agentilor care au sef.
- Permisiunile vin doar din aplicatie (`--settings` inline, `--setting-sources user`). `CLAUDE.md`
  din repo-ul proiectului se citeste in continuare. `git push`, `rm -rf`, `sudo` si instalarile
  de pachete sunt blocate ca prefix de comanda; cu "Comenzi shell" activ, un agent le poate
  ocoli (`rm -fr`, `bash -c ...`). Da shell doar agentilor in care ai incredere (aprobarile
  vin intr-o versiune viitoare).
- Setarile tale globale Claude (`~/.claude/settings.json`) se aplica si agentilor, deci o regula
  globala `permissions.allow` le largeste permisiunile.
- API-ul accepta doar Host 127.0.0.1/localhost si cere `Content-Type: application/json` pe
  request-urile care modifica ceva (protectie impotriva paginilor web care ar trimite cereri
  catre aplicatie).
- Fisierele atasate proiectului stau in `~/.foreman/projects/<id>/files/`; agentii le primesc cu
  `--add-dir` si le citesc cu Read (inclusiv imagini si PDF).
- La pornire toate proiectele sunt pe pauza: nimic nu ruleaza (si nu costa) fara sa apesi "Porneste echipa".

## Date

    ~/.foreman/foreman.db           proiecte, agenti, task-uri, run-uri, mesaje
    ~/.foreman/logs/runs/<id>.jsonl logul brut al fiecarui run
    ~/.foreman/projects/<id>/files/ fisierele atasate proiectului

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
