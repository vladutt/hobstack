# HobStack (fost foreman, initial agentdash)

Tool local, personal (NU SaaS), pentru controlul agentilor Claude Code.
Un singur binar Go instalat pe masina; UI in browser pe 127.0.0.1:7777.
Autorul e senior PHP/Laravel, incepator in Go: cod simplu, idiomatic, comentat unde nu e evident.

## Decizii luate
- Go in loc de Laravel/FrankenPHP: binar unic, daemon natural, goroutines pentru streaming.
- Claude Code CLI headless (`claude -p --output-format stream-json --verbose --resume <sid>`), nu Agent SDK.
- Agentii traiesc in SQLite (~/.hobstack/hobstack.db), per proiect. Fiecare ruleaza in folderul lui (cwd) si scrie doar acolo.
- Echipa e o ierarhie (graf pe canvas): fiecare agent are un sef (`parent_id`) sau e radacina (vorbeste cu utilizatorul).
  Un agent fara sef poate fi si nelegat (`detached`): nu lucreaza si nu primeste mesaje pana il legi pe canvas.
  Un agent cu echipa directa e lider; un sub-lider sparge task-ul primit in sub-task-uri si il raporteaza in sus.
  Optional, un agent are un reviewer (`reviewer_id`): munca trece intai pe la reviewer, apoi la liderul care a creat task-ul.
- Orchestrare prin MCP: aplicatia e server MCP (/mcp/<token>); liderii creeaza si decid task-uri, agentii cu sef raporteaza,
  reviewerii aproba sau resping. Scheduler in internal/team.
- Permisiuni doar din aplicatie: --settings inline + --setting-sources user + --permission-mode dontAsk (fara el, in -p un tool
  nepermis blocheaza run-ul la nesfarsit, verificat cu claude 2.1.285); deny fix pe TaskCreate/TodoWrite/Agent; actiunile riscante (git push, sudo, rm -rf...) vin din regulile pentru comenzi (Settings + proiect, Allow/Ask/Block).
- Numele aplicatiei e config.AppName ("hobstack", afisat config.DisplayName "HobStack"), usor de schimbat.
- Live output prin SSE (nu websockets). UI: HTML + JS vanilla, embedded cu `embed`.
- Fara Docker la runtime; Docker doar pentru dev daca e nevoie.
- Un singur run activ per agent. Stop omoara tot process group-ul.
- Asculta doar pe localhost, fara autentificare.
- Preferinta: dependente minime. SQLite prin modernc.org/sqlite (pure Go, fara CGO).

## Stare
Gata (felia 1, spec in docs/superpowers/specs/2026-09-29-foreman-slice1-design.md): proiecte, echipe,
orchestrare prin MCP, board live, chat per agent, SQLite.
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
Gata (chat in wizard, spec in docs/superpowers/specs/2026-09-30-foreman-wizard-chat-design.md): la pasul 3, panoul
"HobStack" reia sesiunea analistului (--resume) si schimba propunerea la cerere, cu Undo.
Gata (task-uri blocate, spec in docs/superpowers/specs/2026-10-01-foreman-stuck-tasks-design.md): verificare la 1 min;
un task nemiscat 5 min (decizie la un agent idle sau In progress fara run) e impins automat o data, apoi apare in
"Needs you" cu Nudge again / Open <agent>.
Gata (modala de task, spec in docs/superpowers/specs/2026-10-01-foreman-task-modal-design.md): istoric per task
(task_events) inregistrat pe toate drumurile; card compact pe board si modala cu descriere, istoric, raport ascuns si actiuni.
Gata (full autonomy, spec in docs/superpowers/specs/2026-10-01-foreman-autonomy-design.md): comutator per proiect (agentii nu
intreaba, radacina decide in locul utilizatorului, fara bucla la blocaj); cererile de acces din permission_denials apar in
"Needs you" in toate modurile, cu Enable Shell / Dismiss. Task-ul propriu al unei radacini (start_task dintr-o notificare:
fara creator, nu din board, fara etapa; team.ownRootTask) trece singur in Done si fara autonomie, dupa reviewer daca are
("Accepted automatically (the root lead's own task)"); inainte ramanea Blocked la utilizator. Un task creat din board si dat radacinii nu trece singur in Done
nici in modul autonom (decide utilizatorul), iar un run oprit fara report_done (cand agentul il putea chema) nu se accepta automat.
Gata (new chat): butonul "New chat" din chat-ul unui agent sterge sesiunea Claude (urmatorul mesaj porneste una noua) si
ascunde istoricul (agents.chat_from); mesajele vechi raman salvate, vizibile prin "Earlier messages".
Gata (conversatia pe task, spec in docs/superpowers/specs/2026-10-01-foreman-task-conversation-design.md): task_events cu
destinatar si text complet; tab-ul Conversation din modala arata brief-ul, rapoartele, feedback-ul, intrebarile si ce a
spus fiecare agent in run-urile pe task.
Gata (modificarile pe task, spec in docs/superpowers/specs/2026-10-02-foreman-task-changes-design.md): din stream-json
(tool_use + tool_use_result.structuredPatch) se salveaza diff-urile Edit/Write si comenzile Bash per task (task_changes);
eticheta "N files" pe card si tab-ul Changes in modala.
Gata (coloane proprii, spec in docs/superpowers/specs/2026-09-30-foreman-stages-design.md): etape de lucru per
proiect (lucru/verificare, agenti per etapa, primul agent liber ia task-ul); dupa ultima etapa decide liderul
(sau utilizatorul pentru task-urile create din board). Proiectele fara etape merg ca inainte.
Gata (sarcini recurente, spec in docs/superpowers/specs/2026-10-03-foreman-recurring-tasks-design.md): sarcini per
proiect care creeaza task-uri pe un program (la N ore, zilnic, saptamanal; ora locala), pe ticker-ul din team.Run si o data
la pornire (o singura rulare pentru cele ratate); sarite cat timp aparitia precedenta e deschisa; auto-accept optional.
Pachet: internal/schedule.
Gata (Run history, spec in docs/superpowers/specs/2026-10-10-hobstack-recurring-history-design.md): modala per sarcina
recurenta (#/p/<pid>/tasks/recurring/<rid>, din "N runs" pe panoul Recurring): Files (fisierele tuturor rularilor si ale
sub-task-urilor, grupate pe cale, T-N pe versiuni) si Runs (paginat cate 20, excerpt din raport, arhivatele estompate);
store.RecurringHistory / RecurringCounts / RecurringFiles, GET /api/recurring/{id}/history si /files, recurring-history.js.
Gata (Flow, spec in docs/superpowers/specs/2026-10-03-foreman-task-flow-design.md): tab-ul Flow din modala de task arata
arborele de delegare de la radacina (store.TaskFlow, GET /api/tasks/{id}/flow), cu pasii din task_events si sub-task-urile
intercalate; pasul "requested" leaga task-ul de mesajul din chat care l-a pornit (activeRun.request, doar in memorie).
Gata (liderul adauga agenti, spec in docs/superpowers/specs/2026-10-06-foreman-lead-adds-agents-design.md):
tool-urile MCP add_agent / remove_agent pentru lideri si radacini (si fara echipa); agentul intra sub lider, cu
tool-uri din ale liderului, max 6 adaugati; direct cu Full autonomy sau la cererea utilizatorului din chat, altfel
aprobare in "Needs you" (agent_requests); remove = detasare.
Gata (proiecte legate, spec in docs/superpowers/specs/2026-10-06-foreman-project-links-design.md): utilizatorul leaga
proiecte (fara sens); agentii pun intrebari radacinii proiectului legat (ask_project / reply_to_project, max 3 deschise,
anti-bucla) si liderii cer munca (request_project_work: task pentru radacina tinta, aprobat in "Needs you" la tinta,
direct doar la cererea utilizatorului din chat); raportul se intoarce la Done (afterDone).
Gata (fire de lucru): create_task primeste continues ("T-85"); tasks.thread_id (doar afisare) leaga follow-up-urile de task-ul care a pornit munca; pe board doar Done grupeaza (arbore + fir, doar membrii gata); in celelalte coloane fiecare task are cardul lui
(eticheta "↳ T-3" / "follow-up of T-3"), iar "working" vine din run-ul activ (agentView.workingTaskId).
Gata (tool-ul packages): "Install packages" per agent scoate npm install / composer require / pip install / go get
din deny-ul fix (regulile pentru comenzi raman valabile); refuzurile lor apar in "Needs you" cu Allow package installs.
Gata (redesign UI dupa mockup-urile din ../screenshots): tema ardezie rece + font Figtree (static/fonts); bara de sus (ui.topbar)
in loc de sidebar; Proiecte ca tabel; pe proiect board-ul pe toata latimea cu "Needs you" ca prima coloana, Done cu "Show N more";
chat-ul si detaliile agentului intr-un panou lateral care se inchide (starea in localStorage "hobstack-drawer"); modala de task pe
doua coloane (continut + detalii); avatare colorate dupa nume (ui.toneOf, amber = radacina); canvas-ul se incadreaza pe noduri.
Echipa nu mai e in bara de sus: avatarele stau langa filtrele board-ului (clic = chat cu agentul); peste TEAM_SHOWN (5)
se vad primii 4 si "+N", care deschide lista restului.
Gata (Stats, spec in docs/superpowers/specs/2026-10-08-foreman-project-stats-design.md): tab-ul Stats pe proiect
(Today / 7d / 30d / All): cheltuiala si tokeni pe agent si pe zi, task-uri create/terminate, mediana pana la Done,
rata de succes a run-urilor, fisiere si comenzi, intrebari; store.ProjectStats + GET /api/projects/{id}/stats, grafice SVG in stats.js.
Gata (fara pauza la pornire): la restart run-urile vechi devin interrupted, task-urile in lucru revin in Queued si
echipele pornesc singure (Recover trezeste proiectele si decidentii). Pauza o pune doar utilizatorul (Stop team) sau un run
care nu poate porni (anti-bucla); migrarea 020 a scos o data proiectele de pe pauzele puse de restarturile vechi.
Gata (draft AI pentru agent nou, spec in docs/superpowers/specs/2026-10-08-foreman-agent-draft-design.md): pachetul
internal/draft, run scurt doar citire cu tool-ul propose_agent; pagina Agent nou completeaza campurile neatinse.
Costul nu intra in Total cost.
Gata (Left for you, spec in docs/superpowers/specs/2026-10-08-foreman-left-for-you-design.md): report_done
left_for_user, tick_left_for_user, comment_task; comentariile utilizatorului trezesc asignatul; task_todos
(migrarea 021); cardul To fill in in Needs you.
Gata (Library, spec in docs/superpowers/specs/2026-10-08-foreman-library-design.md): tool-uri, skill-uri si servere MCP
refolosite intre proiecte. Tabelele library_* (migrarea 022); pachet internal/library: propuneri copiate cu OpenRoot,
versiuni read-only, plugin per proiect pe hash (--plugin-dir), serverele MCP prin --mcp-config (fisier 0600 cu
secrete), probe la Publish (30 s; fara probe pentru elementele facute de utilizator; "Publish without test" dupa confirmare).
Tool-ul MCP propose_library_item (MCP local doar cu Shell). API + UI #/library,
Instructions > Library cu secrete per proiect, "Recent use" pe pagina item-ului (tool ancorat pe <build>/<pid>-<hash>/bin/<nume>/ in comenzile
Bash, MCP dupa mcp__<nume>__*, doar run-uri pe task).
Gata (reguli pentru comenzi, spec in docs/superpowers/specs/2026-10-08-foreman-command-rules-design.md): command_rules
(globale + override pe proiect, migrarea 023), Allow / Ask / Block pe prefixe pentru agentii cu Shell, pagina Settings;
refuzurile pe Ask ajung in Needs you cu Allow once (command_grants) / Allow in this project / Block.
Gata (aprobari pe task, spec in docs/superpowers/specs/2026-10-09-foreman-task-grants-design.md): un grant din Needs you
(command_grants.task_id, migrarea 029) tine pe toate run-urile agentului pe task (NULL = chat, pana la New chat) si intra in
--settings ca Bash(prefix:*) in allow ("Allow once" a devenit "Allow "<prefix>" on T-N"); agentii fara Shell primesc cereri pe
prefix calculat (prompt.NoShellRequests / CommandPrefix, access_requests.kind 'command'); Enable Shell / Allow package installs
trezesc agentul (team.EnableTool); un task cu cerere de acces deschisa nu e "stuck", nu merge in review cand run-ul se
opreste fara report_done si nu e repus in coada la Stop team / restart; promptul (howDenied) spune ca permisiunile vin din
HobStack si ca agentul nu cere aprobari in chat. Fara card pentru comenzile pe care claude le permite singur (cd, ls, cat, head,
tail, grep... verificat cu claude 2.1.295) si fara prefix pentru interpretoare/wrapper-e sau `git -C` (raman Enable Shell);
`cd dir && git ...` e refuzat de claude fara Shell chiar cu Bash(cd:*). Instalarile (npm i, yarn/pnpm/bun add, pip3, cargo,
gem, composer install, go install) sunt in refuzul fix fara tool-ul packages.
Gata (chat doar de citire, spec in docs/superpowers/specs/2026-10-09-foreman-chat-read-only-design.md): un run fara task
(chat, notificari, comentarii) primeste prompt.ReadOnlyTools (fara write/packages, git doar citire: git-read; shell ramas pentru verificari, prin regulile
pentru comenzi, cererile Ask sunt de chat si grant-urile de chat intra in run) si linia prompt.ReadOnlyTurn (comenzi doar
care verifica: loguri, cozi, docker exec, teste); system prompt-ul ramane acelasi (sesiunea continua). Modificarile se fac pe task: tool-ul MCP
start_task (doar in run-urile fara task) creeaza un task Queued pentru agent, fara etape, CreatorID gol, ByUser pentru
cererile din chat, max 3 in lucru (Queued / In progress; evenimentul self_started); cu echipa oprita raspunsul spune
ca task-ul porneste la Start team. Comentariile pe task cer modificarile printr-un task de continuare. Refuzurile Edit/Write (si Bash fara shell) din run-urile fara task nu fac
cereri de acces. Verificat cu claude 2.1.295 (agent fara shell): Edit, redirectarea >, tee si git commit sunt refuzate, git log trece.
Gata (cod colorat si fisierele modificate, spec in docs/superpowers/specs/2026-10-09-foreman-code-view-design.md):
highlight.js 11.9.0 (build common ES, 36 de limbaje, static/vendor/highlight, BSD-3) prin static/js/highlight.js (languageFor,
highlightInto, highlightLine; fara colorare peste 5000 linii / 500 KB) in markdown, diff-uri (changes.js: diffBlock) si
previzualizari. Tab-ul Modified pe task (GET /api/tasks/{id}/modified, cu sub-task-urile) si modala de fisier (file-view.js,
GET /api/tasks/{id}/file?path=&task=): fisierul de acum de pe disc, doar cai din task_changes, prin os.OpenRoot, max 1 MB,
binar = NUL in primii 8 KB; marcajele added/modified/deleted din store.FileMarkers (fragmentele aplicate in ordine;
exact=false fara numere de linie / trunchiat / Write fara fragmente). Deschisa din Modified, "View file" din Changes si
numele de fisiere +/- din rapoarte; Esc inchide doar modala de fisier.
Gata (fisierele doar sus): rezumatul "Changes" de deasupra tab-urilor e singura lista de fisiere (GET /modified, cu
sub-task-urile; primele 3, "N more files" le desfasoara) si deschide direct modala de fisier; tab-urile Changes si Modified au
disparut, comenzile rulate sunt in tab-ul Commands (doar cand exista). Modala de fisier are in stanga lista fisierelor
modificate (grupate pe foldere, sagetile sus/jos trec intre ele), File/Diff ramane la schimbarea fisierului.
Gata (explorer de cod, spec in docs/superpowers/specs/2026-10-12-hobstack-code-explorer-design.md): Cmd/Ctrl+P sau butonul
Code pe paginile proiectului deschide code-explorer.js (doar citire): arbore pe foldere (desenare lenesa, starea in
localStorage "hobstack-code-<pid>"), cautare dupa nume, cod colorat. GET /api/projects/{id}/tree (lista de la @, cu
truncated/error) si /code-file?folder=&path= (doar cai din lista, cache invalidat o data, readIn + isBinary). Clic / Shift+clic
pe numere selecteaza linii: Copy reference (cale:42-50, prefixul folderului cu mai multe foldere), Ask <agent> pune
"@cale:42-50 " in chat (prefillChat; calea ca la @). Cmd+C fara text selectat = referinta, Cmd+Shift+C = calea.
Scurtaturile folosesc modKey (Cmd pe macOS, Ctrl in rest). Un fisier sters dispare din lista (cache invalidat la 404,
git ls-files --deleted scazut); un submodul / fisier special vine cu notRegular (readIn refuza ce nu e fisier obisnuit).
Cautare in fisier in explorer (Cmd/Ctrl+F sau Find): "N of M", Enter / Shift+Enter, Aa, max 2000 rezultate; potrivirile
se marcheaza cu <mark> in nodurile de text deja colorate (wrapRange), Cmd/Ctrl+Enter selecteaza linia rezultatului (Copy
reference / Ask); se reia la schimbarea fisierului, Esc inchide doar cautarea.
Gata (switcher de proiecte): Cmd/Ctrl+K pe orice pagina (sau butonul ⌘K de langa logo) deschide project-switcher.js:
fara text, proiectele deschise recent primele (localStorage "hobstack-recent-projects", max 20, actualizat in app.js la intrarea
pe #/p/<id>), apoi favoritele, cel curent ultimul; cu text, numele incepe / contine, apoi categoria. Enter pastreaza tab-ul
(Stats -> Stats), Cmd/Ctrl+Enter in tab nou; sta peste explorer-ul de cod (explorer-ul si modala de fisier ignora tastele din .ps-overlay).
Gata (fisiere pe task, spec in docs/superpowers/specs/2026-10-08-foreman-task-files-design.md): report_done files
(max 20) copiaza livrabilele ca snapshot in projects/<pid>/task-files/<task>/ (task_files, migrarea 024; pachet
internal/taskfiles, doar din folderele proiectului si projects/<pid>/files, prin os.OpenRoot); evenimentul files_attached; tab-ul Files din
modala (preview imagine/PDF/Markdown/text, Download, versiuni, "From subtasks"), eticheta 📎 N pe card; HTML/SVG nu
se servesc inline (serveStored, comun cu fisierele urcate).
Gata (fisierele utilizatorului pe task): utilizatorul pune fisiere pe task in formularul de task nou (Attach files / drag & drop,
POST /api/projects/{id}/tasks multipart) si in tab-ul Files din modala (mereu vizibil, Upload / drop, fara upload pe task-urile
sterse; POST /api/tasks/{id}/files, 50 MB / 200 MB ca la proiect). Tot in task_files, fara migrare: agent_id NULL si autorul
"You" (store.UserFileAuthor, TaskFile.FromUser); GET /files le intoarce separat ("user", sectiunea "From you"). Fisierele se
scriu intai in staging (task-files/.staging-*, taskfiles.Save), iar team.UserCreateTaskWithFiles / AddUserFiles le muta cu
rename si le adauga sub t.work inainte de Kick (la eroare task-ul abia creat se sterge); max 50 pe task (TooManyFilesError,
400). Tura pe task are blocul "Files from the user" (prompt.TaskTurn, max 50 randuri): ale task-ului, apoi ale firului
(ThreadID) si ale parintelui, fara dubluri. Orice run al proiectului primeste --add-dir pe projects/<pid>/task-files. Upload pe
un task in lucru / blocat: nota in coada cu caile ("- <cale> (<nume>)") + Kick; review / done: doar evenimentul; in coada:
nimic. Evenimentul user_files (cu caile) intra in istoric si in conversatie. Numele de agent "You" e rezervat
(store.ReservedAgentName: CheckAgentFields, propunerea din wizard, draft-ul).
Gata (cont Claude per proiect, spec in docs/superpowers/specs/2026-10-09-foreman-claude-accounts-design.md):
claude_accounts (migrarea 025; token din `claude setup-token`, camp privat, nu iese din API) + projects.account_id (NULL =
Default, CLI-ul logat). runner.Spec.Env pune CLAUDE_CODE_OAUTH_TOKEN si scoate variabilele de cont mostenite (doar pe
proiectele cu cont; Default mosteneste mediul neatins). Settings > Claude accounts: Check (runner.Check, haiku fara setarile
utilizatorului, sub o zecime de cent) si claude auth status pentru Default. Schimbarea contului cere echipa oprita si
reseteaza sesiunile (ca New chat); o eroare de autentificare (runner.IsAuthError) pune proiectul pe pauza, task-ul inapoi in
Queued si cardul "Claude account failed" in Needs you. Wizard-ul alege contul la pasul 1; draft-ul si wizard-ul pe folder
folosesc contul proiectului.
Gata (mutare si stergere, spec in docs/superpowers/specs/2026-10-09-foreman-task-move-delete-design.md): drag & drop pe
board = actiunea coloanei (team.MoveTask: Accept, Send back, Unblock, Reopen, Close, mutare intre etape; In progress si
Review nu primesc drop), meniul "⋯" de pe card (Move to… / Delete). Delete = arhivare (status archived, migrarea 026,
archived_from/archived_root): run-ul se opreste, intrebarile/cererile legate se inchid, ListTasks/Subtasks nu le mai
intorc; toast cu Undo si panoul "Deleted (N)" cu Restore.
Gata (@ in chat): in chat-ul cu un agent, @ deschide lista fisierelor de cod ale proiectului (pachet internal/codefiles:
git ls-files cu .gitignore, altfel parcurgere fara node_modules/vendor/dist/build/foldere ascunse; max 20000 pe folder,
cache 30 s; GET /api/projects/{id}/code-files?q=&agent=); alegerea scrie @cale (relativa in folderul agentului, absoluta
in celelalte), agentul citeste singur fisierul. UI: static/js/mentions.js.
@ merge si in comentariul de pe task: ?task= pune primele fisierele modificate de task si de sub-task-uri (eticheta
"changed", cele mai recente primele, fara dubluri), cu caile relative la folderul agentului care primeste comentariul.
Gata (limitele contului pe Stats): runner-ul retine ultimul rate_limit_event din stream-json (fereastra de 5 ore si cea
saptamanala: utilization + resetsAt, status allowed/allowed_warning/rejected) in Outcome.RateLimit; team.finish il salveaza
pe contul proiectului (account_usage, migrarea 028, account_id 0 = Default); GET /api/projects/{id}/stats are "usage" si
"account"; cardul "Claude usage" sus pe Stats (bare, % ramas, ora resetului, "Updated … ago").
Gata (categorii de proiecte): projects.category (migrarea 027, text liber, max 40; "work" ia scrierea unei categorii
existente). Pe Proiecte: filtre All / <categorie> / Uncategorized (alegerea in localStorage "hobstack-category") si
gruparea tabelului pe categorii; cautarea cauta si in categorie. Campul Category (category.js, categoryInput) are lista
categoriilor existente cu numarul de proiecte, deschisa la focus, plus "Create "X""; e in formularul de proiect si la pasul 3
din wizard (POST /api/projects/setup primeste category). Eticheta categoriei pe rand (vizibila cand filtrezi sau cauti; click
= filtru) si in header-ul proiectului (click = Proiecte filtrate).
Gata (decizii usor de dat, spec in docs/superpowers/specs/2026-10-09-foreman-easy-decisions-design.md): report_done primeste
decisions (max 5 intrebari cu 2-5 optiuni, multi) si user_steps (max 10, in task_todos cu kind step); task_decisions (migrarea
030). Cand decide utilizatorul, modala arata Decide (radio/checkbox + Other), Your steps si zona de actiune (nota, motive rapide,
Accept / Send back mari); POST /decide primeste answers: la Accept fiecare agent care a intrebat primeste in coada doar
raspunsurile lui (si nota), la Send back raspunsurile intra in feedback; evenimentul decided dupa decizie; raspuns identic
retrimis = no-op.
Gata (install MCP din registru, spec in docs/superpowers/specs/2026-10-09-foreman-mcp-install-design.md): pachetul
internal/mcpregistry cauta in registrul oficial (GET /v0/servers, cache 10 min, timeout 10 s, HOBSTACK_MCP_REGISTRY_URL
doar http/https) si construieste comanda (npx/uvx/docker, fara runtimeArguments optionale; argumente sau header-e
obligatorii = neinstalabil) si citeste serverele din ~/.claude.json si Claude Desktop cu valorile credentialelor ascunse
(<hidden>, serverul devine neinstalabil). Library > MCP > + Add server (#/library/mcp/add[?project=]): cautare, From
your Claude, Install cu nume, comanda exacta confirmata, valorile cheilor pentru proiect (secretValues, folosite la
proba) si Install without test; POST /api/library/install-mcp creeaza elementul publicat si il instaleaza in proiect
atomic.
Gata (redenumire in HobStack): modulul Go, cmd/hobstack, binarul, textele si prompturile (HOBSTACK_MCP_REGISTRY_URL,
X-HobStack). La prima pornire config.MoveLegacyData muta ~/.foreman in ~/.hobstack (foreman.db -> hobstack.db, cu -wal/-shm)
si lasa un symlink pe calea veche; store.RewriteDataRoot inlocuieste calea in instructiunile agentilor, brief-uri, sarcinile
recurente si task_changes.path. Istoricul ramane cum a fost scris (evenimentele vechi au actorul "Foreman"; doi agenti reali se
numesc asa). Cheile din localStorage "foreman-*" trec pe "hobstack-*" in app.js. Folderul implicit pentru proiecte noi: ~/HobStack.
Gata (catalog MCP local, spec in docs/superpowers/specs/2026-10-10-hobstack-mcp-catalog-design.md): registrul oficial
raspunde in 5-40 s la cautarile noi, deci mcpregistry.Catalog tine ultima versiune a fiecarui server in
~/.hobstack/mcp-registry.db (fisier separat, schema cu user_version, refacut daca e stricat). Prima sincronizare completa
pagina cu pagina (full_cursor + full_started in meta, reluata dupa restart; cursor respins = reia de la pagina 1), apoi
actualizare cu updated_since = last_sync_at - 1 h (deleted/deprecated si versiunile refuzate de normalize scot randul).
Goroutine in cmd/hobstack: la pornire si la fiecare ora daca Due (24 h). GET /api/library/registry cauta local cand e
gata (cuvinte cu instr, nume exact primul, 30 pe pagina), altfel la registru cu timeout 60 s; raspunsul are catalog
(state/count/updatedAt/syncing/lastError); /registry/status si POST /registry/sync (202/409). UI: randul de stare cu
Update now (poll la 3 s cat ruleaza), cautare anulabila (api() primeste signal; erorile au status si data).
Gata (Add skill din Claude si skills.sh, spec in docs/superpowers/specs/2026-10-10-hobstack-skill-install-design.md): limite 200 de
fisiere / 10 MB pentru skill-uri; library_sources (migrarea 031) tine sursa elementului. Pachet internal/skillsrc: cautare skills.sh,
lista Official din anthropics/skills, Fetch in doua treceri fixat pe commit-ul folderului, surse locale (Claude Code, claude.ai
sincronizat, plugin-uri), Fingerprint. Endpointuri /api/library/skills/*; folderele pregatite au token de 30 min; instalarea
publica elementul si optional il activeaza; source/check|prepare|update inlocuiesc versiunea cu PublishReplaceStaged sub lock-ul
elementului. UI #/library/skill/add (Official / From your Claude), avertismente pentru script-uri si continut sensibil, "Update
available" cu diff, verificare zilnica a surselor (CheckSources, la pornire si din ora in ora; ErrRateLimited opreste bucla).
Gata (fara review cand nu s-a schimbat nimic): report_done sare peste reviewer daca run-ul curent n-a modificat fisiere si
n-a rulat comenzi (runner.Manager.Activity, numarat din stream-json in timpul run-ului) si ultima decizie a reviewer-ului pe
task a fost approved (team.skipReview); raportul merge la decident, evenimentul review_skipped. Dupa o respingere, la primul
raport sau cu activitatea necunoscuta, reviewer-ul il primeste ca inainte.
Gata (imagini lipite): Cmd+V cu o imagine in chat (deja era), in formularul New task (adauga la Files) si oriunde in modala de
task (urca in Files, ca Upload; nu pe task-urile sterse); static/js/paste.js (onPasteFiles: textul se lipeste normal, numele
generic "image.png" devine screenshot-HHMMSSmmm.png; fileThumb / releaseThumb: miniaturi pe URL blob in chip-uri si in lista).
Gata (modificari prin shell): agentii cu write primesc in prompt regula howEdits (fisierele se schimba doar cu Edit / Write,
nu cu cat >, heredoc, sed -i sau scripturi). Plasa de siguranta, pachetul internal/gitsnap: la pornirea unui run pe task care
poate scrie, Take retine HEAD-ul si fisierele deja modificate din folderul agentului (cu continutul, max 1 MB / 32 MB); la final
(team.shellChanges, inainte de lock) Changes compara cu starea de acum (git status + commit-urile facute in run, hash-object,
diff prin git diff --no-index) si adauga fisierele schimbate altfel, cu Tool "Shell" (create / edit / delete, max 100). Fara repo
git nu face nimic; fisierele ignorate nu se vad; un agent care lucreaza in acelasi folder in acelasi timp poate aparea si el.
GIT_OPTIONAL_LOCKS=0 ca sa nu ia index.lock. UI: eticheta "shell" (shellTag) in Changes si in lista modalei de fisier (modifiedView.shell).
Gata (chat lizibil): mesajele utilizatorului raman text simplu, dar blocurile ``` devin cod colorat (md.userText); un mesaj al
utilizatorului peste 12 linii / 1200 de caractere si blocurile de cod ale agentilor peste 18 linii se arata restranse (md.collapsible,
clasele .fold*: 10 / 14 linii, "Show all · N lines"). Campul de scris creste cu textul pana la min(45vh, 360px).
Gata (mai multe fisiere lipite): Firefox da paginii doar primul fisier copiat din Finder / Explorer (verificat cu Firefox 157;
Chrome le da pe toate). La paste cu fisiere, paste.js (fromSystem) cere serverului fisierele din clipboard-ul sistemului
(pachetul internal/clipfiles: JXA / NSPasteboard pe macOS, PowerShell Get-Clipboard -Format FileDropList pe Windows,
wl-paste / xclip text/uri-list in rest; max 50) prin POST /api/clipboard/files si /api/clipboard/file (doar POST cu JSON;
continutul doar pentru o cale aflata chiar atunci in clipboard, fisier obisnuit, max 50 MB). Se iau de acolo doar daca sunt
mai multe decat a dat browserul si printre ele e primul fisier al browserului; altfel (captura de ecran, eroare) raman ale
browserului. Merge in chat, New task si modala de task.
Gata (liderul si sarcinile recurente, spec in docs/superpowers/specs/2026-10-16-hobstack-lead-recurring-design.md):
liderii si radacinile au list_recurring / create_recurring / update_recurring / delete_recurring (team/recurring_tools.go);
schimbarile doar intr-un run pornit dintr-un mesaj al utilizatorului (requestOf; Full autonomy nu ajunge), doar pentru ei
insisi, echipa directa sau prima etapa (radacina), si doar pe sarcinile din zona lor. Aparitiile merg ca la sarcinile
utilizatorului. recurring_tasks.created_by (migrarea 032), "Created by <agent>" in panoul Recurring; trim-ul si limitele
titlului / descrierii sunt in team (RecurringInput.clean), comune cu UI-ul.
Gata (pasii pentru utilizator doar cand chiar trebuie): user_steps (report_done) sunt doar ce agentul n-a putut face si
poate doar un om (verificare vizuala, cont, dispozitiv); comenzile, testele si verificarile le ruleaza singur si le spune in
raport (howLeftForUser, descrierea user_steps). Cu Full autonomy promptul adauga howAutoSteps, iar pasii (task_todos kind
step) nu mai intra in "need you" (store.ListFillIns(pid, withSteps=!autonomy)); raman pe task, in Your steps.
Gata (shell pentru agentii de cod): wizard-ul (setup.safeTools = write, shell, git) propune shell agentilor care scriu sau
testeaza cod, ca sa-si ruleze testele si build-ul; web ramane doar la utilizator (si in chat-ul wizard-ului). Agentul nou de pe
canvas porneste cu write + shell, ca formularul Agent nou.
Gata (fisierele commit-uite pe task): gitsnap.Changes intoarce si fisierele doar intrate intr-un commit facut in run (deja
modificate la pornire, acelasi continut; HEAD-ul nou descinde din cel vechi, deci nu reset/checkout inapoi), cu diff-ul din
commit (Change.Committed, Tool "Commit" in task_changes); in UI eticheta "commit" (commitTag, modifiedView.commit).
Gata (scrieri in afara folderului): un refuz Edit / Write / MultiEdit / NotebookEdit pe o cale absoluta din afara folderului
agentului (team.writeOutside) nu mai face cerere de acces in Needs you (n-are ce aproba utilizatorul), ramane doar evenimentul
denied pe task; howEdits spune ca fisierele temporare si scripturile de test stau tot in folderul agentului, nu in /tmp sau ~/.hobstack.
Gata (coloanele proprii langa cele standard, spec in docs/superpowers/specs/2026-11-02-hobstack-stages-beside-columns-design.md):
board-ul are mereu Queued | In progress | <etape> | Review | Done; etapele sunt opriri optionale (din board: Assignee
"<etapa>: first free agent" sau "+ Add a task" pe coloana; liderul: create_task cu stage). tasks.via_stages (migrarea 033):
Send back intoarce in etapa doar task-urile trecute prin etape; tras pe Queued, task-ul iese din etape.
Gata (titlul tab-ului): "<Proiect> · <Sectiune> — HobStack" (app.js updateTitle).
Gata (versiuni si update din aplicatie, spec in docs/superpowers/specs/2026-11-05-hobstack-self-update-design.md):
taguri vX.Y.Z (prima v1.0.0), config.Version prin -ldflags (go build simplu = "dev"; scripts/build.sh pune git describe),
`hobstack version`. La push-ul unui tag, .github/workflows/release.yml ruleaza testele si scripts/build-release.sh (arhive
hobstack_<ver>_<os>_<arch>.tar.gz/.zip + checksums.txt) si publica release-ul (notele = mesajul tag-ului adnotat). Pachetul
internal/selfupdate: releases/latest de pe GitHub (config.UpdateRepo) la pornire + din ora in ora, Newer pe semver; Update:
sha256 din checksums.txt, binarul in <exe>.new, proba `version`, vechiul -> .prev, team.Hold pana nu mai ruleaza niciun run,
copie VACUUM INTO in ~/.hobstack/backups (ultimele 3), restart prin selfupdate.Reexec cu --no-open. UI (update.js): modala la
intrare cu versiunea si notele (Later / Skip this version), insigna din bara de sus (ui.setTopbarExtra), Settings > Version;
/api/update*.
Gata (Claude Code din aplicatie, spec in docs/superpowers/specs/2026-11-05-hobstack-claude-setup-design.md): HobStack porneste
si fara claude (claudecli.Find: PATH, ~/.local/bin, npm global...; team.Hold e contor, impartit cu update-ul; runner.SetClaude
dupa instalare, runner.ErrNoClaude cat lipseste). Pagina de start claude-setup.js: Install (installerul oficial, output live),
Add to PATH (Windows: PATH-ul de utilizator; macOS/Linux: ~/.zshrc / ~/.bash_profile), Log in (terminal cu claude auth login);
/api/claude*. Fara comanda (dublu-clic) binarul face start; la eroare intr-o consola proprie (Windows) asteapta Enter.
Gata (iconita, fara terminal, pornire la login, spec in docs/superpowers/specs/2026-11-05-hobstack-background-design.md):
assets/hobstack.ico + cmd/hobstack/rsrc_windows_amd64.syso (scripts/winres, doar stdlib), favicon.png. Pe Windows, cu consola
doar a lui, HobStack o ascunde si scrie in ~/.hobstack/hobstack.log (erorile in MessageBox); pornit a doua oara deschide instanta
care ruleaza; iconita langa ceas (cmd/hobstack/tray_windows.go, Shell_NotifyIconW: clic = Open, meniu Open / Quit).
internal/autostart (Run key / LaunchAgent app.hobstack / XDG autostart; pornita implicit o data de o versiune de release,
marcajul autostart-chosen din folderul datelor pastreaza alegerea) si Quit in Settings > App (/api/app*).
Installer Windows: installer/hobstack.nsi (NSIS, fara admin, folder la alegere, scurtaturi Desktop / Start menu, uninstaller),
construit in scripts/build-release.sh; hobstack.portable langa exe = datele in <folder>/data (config.InstallDir, o mutare
din ~/.hobstack prin config.MoveToInstall); `hobstack quit [--force]` opreste instanta care ruleaza.
Gata (macOS si Linux ca pe Windows, spec in docs/superpowers/specs/2026-11-05-hobstack-background-design.md): macOS:
bara de meniu (cmd/hobstack/menubar_darwin.go, cgo + Cocoa; start intr-o goroutine, [NSApp run] pe firul principal),
HobStack.app universal in HobStack-<v>.dmg (scripts/build-mac.sh, job `mac` pe macos-latest; afterSwap re-semneaza
bundle-ul dupa update), PATH din shell-ul de login cand e pornit fara terminal. Linux: tray prin fyne.io/systray
(StatusNotifierItem) si `hobstack install` / `uninstall` (meniu, Desktop, ~/.local/bin, datele langa exe). Dupa
pornirea unei versiuni de release, <exe>.prev se sterge (selfupdate.CleanupPrevious).
Gata (fisierele proprii, spec in docs/superpowers/specs/2026-11-05-hobstack-own-files-design.md): un agent sterge fara
sa intrebe ce a creat el (task_changes kind create, in folderul lui, necommis in git): tool-ul MCP delete_files si, pentru
un `rm` simplu oprit de Ask pe fisiere doar ale lui, HobStack sterge in locul lui fara card (team/own_files.go).
Urmeaza (sub-proiecte separate, fiecare cu spec + plan):
2. Librarie de agenti refolositi intre proiecte, template-uri de echipa
3. Aprobari live (--permission-prompt-tool), feed global "Live activity"
4. Runs & logs, Settings
Dupa: service install, goreleaser, notificari, git worktrees.
Idei salvate (fara spec): chat general pe orice pagina, care propune modificari cu Apply/Undo (docs/ideas/2026-10-07-general-assistant.md);
admin self-hosted al companiei, doar limite si setari, fara comenzi pe masini (docs/ideas/2026-10-07-company-admin.md);
plugin JetBrains subtire (UI-ul web in JCEF + deschis fisiere/diff-uri in IDE) (docs/ideas/2026-10-07-jetbrains-plugin.md).

## Conventii
- `go vet ./...` si `go build -race` trebuie sa treaca.
- JS-ul din static/ se verifica ca modul: copiaza fisierul ca .mjs si ruleaza `node --check` (pe .js nu prinde erorile de sintaxa).
- Verifica cross-compile: `GOOS=darwin GOARCH=arm64 go build ./cmd/hobstack` si `GOOS=windows go build ./cmd/hobstack`.
- Tot ce vede utilizatorul (UI, mesaje, erori) si prompturile pentru agenti sunt in engleza. Comentariile din cod raman in romana.
