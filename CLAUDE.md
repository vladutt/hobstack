# foreman (fost agentdash)

Tool local, personal (NU SaaS), pentru controlul agentilor Claude Code.
Un singur binar Go instalat pe masina; UI in browser pe 127.0.0.1:7777.
Autorul e senior PHP/Laravel, incepator in Go: cod simplu, idiomatic, comentat unde nu e evident.

## Decizii luate
- Go in loc de Laravel/FrankenPHP: binar unic, daemon natural, goroutines pentru streaming.
- Claude Code CLI headless (`claude -p --output-format stream-json --verbose --resume <sid>`), nu Agent SDK.
- Agentii traiesc in SQLite (~/.foreman/foreman.db), per proiect. Fiecare ruleaza in folderul lui (cwd) si scrie doar acolo.
- Echipa e o ierarhie (graf pe canvas): fiecare agent are un sef (`parent_id`) sau e radacina (vorbeste cu utilizatorul).
  Un agent fara sef poate fi si nelegat (`detached`): nu lucreaza si nu primeste mesaje pana il legi pe canvas.
  Un agent cu echipa directa e lider; un sub-lider sparge task-ul primit in sub-task-uri si il raporteaza in sus.
  Optional, un agent are un reviewer (`reviewer_id`): munca trece intai pe la reviewer, apoi la liderul care a creat task-ul.
- Orchestrare prin MCP: aplicatia e server MCP (/mcp/<token>); liderii creeaza si decid task-uri, agentii cu sef raporteaza,
  reviewerii aproba sau resping. Scheduler in internal/team.
- Permisiuni doar din aplicatie: --settings inline + --setting-sources user + --permission-mode dontAsk (fara el, in -p un tool
  nepermis blocheaza run-ul la nesfarsit, verificat cu claude 2.1.285); deny fix pe actiuni riscante si pe TaskCreate/TodoWrite/Agent.
- Numele aplicatiei e config.AppName ("foreman"), usor de schimbat.
- Live output prin SSE (nu websockets). UI: HTML + JS vanilla, embedded cu `embed`.
- Fara Docker la runtime; Docker doar pentru dev daca e nevoie.
- Un singur run activ per agent. Stop omoara tot process group-ul.
- Asculta doar pe localhost, fara autentificare.
- Preferinta: dependente minime. SQLite prin modernc.org/sqlite (pure Go, fara CGO).

## Stare
Gata (felia 1, spec in docs/superpowers/specs/2026-09-29-foreman-slice1-design.md): proiecte, echipe,
orchestrare prin MCP, board live, chat per agent, SQLite, pauza la pornire.
Gata (echipe ca graf, spec in docs/superpowers/specs/2026-09-29-foreman-team-graph-design.md): ierarhie cu mai multe
radacini si sub-lideri, reviewer per agent, editor canvas.
Gata (wizard, spec in docs/superpowers/specs/2026-09-30-foreman-setup-wizard-design.md): "Proiect nou cu AI":
un analist Claude (doar citire) citeste folderul si propune proiectul si echipa prin tool-ul MCP propose_setup;
utilizatorul editeaza propunerea si creeaza totul intr-o tranzactie. Pachet: internal/setup.
Analiza ruleaza in fundal (sesiuni doar in memorie): apare pe Proiecte ca un card (Analyzing / Ready / Failed), redeschis prin #/setup/<id>.
Gata (foldere, spec in docs/superpowers/specs/2026-09-30-foreman-multi-folder-design.md): un proiect are unul sau mai multe
foldere (tabel project_folders; primul e implicit). Fiecare agent lucreaza si scrie in folderul lui (agents.folder_id,
NULL = primul), citeste restul prin --add-dir. Wizard-ul propune echipe si context per folder.
Gata (wizard pe folder, spec in docs/superpowers/specs/2026-09-30-foreman-folder-setup-design.md): "Set up agents with AI"
pe un folder al unui proiect existent; analistul propune contextul folderului si agenti noi sub un agent existent,
adaugati cu store.AddAgents.
Gata ("Needs you", spec in docs/superpowers/specs/2026-09-30-foreman-needs-you-design.md): tool-ul MCP ask_user;
intrebarile deschise apar pe proiect, pe card, in chat si pe agent; raspunsul trezeste agentul si pune task-ul inapoi in
coada (un task care asteapta raspuns nu e re-pus in coada la Stop team / restart).
Gata (coloane proprii, spec in docs/superpowers/specs/2026-09-30-foreman-stages-design.md): etape de lucru per
proiect (lucru/verificare, agenti per etapa, primul agent liber ia task-ul); dupa ultima etapa decide liderul
(sau utilizatorul pentru task-urile create din board). Proiectele fara etape merg ca inainte.
Urmeaza (sub-proiecte separate, fiecare cu spec + plan):
2. Librarie de agenti refolositi intre proiecte, template-uri de echipa
3. Aprobari pentru actiuni riscante (--permission-prompt-tool), feed global "Live activity"
4. Runs & logs, Settings
Dupa: cron-uri, service install, goreleaser, notificari, git worktrees.

## Conventii
- `go vet ./...` si `go build -race` trebuie sa treaca.
- JS-ul din static/ se verifica ca modul: copiaza fisierul ca .mjs si ruleaza `node --check` (pe .js nu prinde erorile de sintaxa).
- Verifica cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- Tot ce vede utilizatorul (UI, mesaje, erori) si prompturile pentru agenti sunt in engleza. Comentariile din cod raman in romana.
