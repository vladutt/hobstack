# Brainstorm Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tab-ul "Brainstorm" pe proiect: subiecte ca niste canale, la fiecare mesaj al utilizatorului fiecare participant raspunde cu un card (idee, idee wild sau intrebare), utilizatorul alege, iar ce a ales ramane, intra in Files sau merge la lider.

**Architecture:** Date noi in SQLite (migrarea 038, `internal/store/brainstorm.go`). Un tip nou de run in `internal/team`: run fara task, doar citire, cu sesiune Claude separata per (subiect, agent) si un singur tool MCP, `brainstorm_reply`. Reconcile porneste cardurile doar cand agentul e liber (sau inaintea cozii, cu "Run now"). API in `internal/web/api_brainstorm.go`, UI in `static/js/brainstorm.js`, live prin evenimentul SSE "brainstorm".

**Tech Stack:** Go, SQLite (modernc.org/sqlite), JS vanilla embedded, SSE (internal/events).

**Spec:** `docs/superpowers/specs/2026-11-11-hobstack-brainstorm-design.md`

## Global Constraints

- Tot ce vede utilizatorul (UI, erori) si prompturile in engleza; comentariile din cod in romana.
- `go vet ./...` si `go build -race ./...` trec; cross-compile: `GOOS=windows go build ./cmd/hobstack`.
- JS: fiecare fisier modificat din static/ copiat ca .mjs si verificat cu `node --check`.
- Atasamente: 50 MB / fisier, 200 MB / request (`readUploads`), max 50 pe subiect.
- Un singur run activ per agent; run-urile de brainstorming sunt doar citire (`prompt.ReadOnlyTools`).
- Intrebarile agentilor NU apar in "Needs you"; doar insigna pe tab-ul Brainstorm.
- Max 2 intrebari pe agent pe runda. Intrebare: 2-5 optiuni + "Other" in UI.
- Full autonomy nu schimba nimic in brainstorming.

## Review Focus

- Un agent mereu ocupat (lider cu coada plina): cardul asteapta, dar "Run now" il porneste dupa run-ul curent, inaintea urmatorului task — test in Task 5.
- Agent scos din participanti sau sters cu un card in asteptare: cardul devine "skipped", nu ramane agatat — test in Task 5.
- Restart cu un card "running": devine "waiting" si porneste din nou — test in Task 5.
- Agentul termina run-ul fara `brainstorm_reply` sau il cheama de doua ori: textul final devine idee; al doilea apel e refuzat — teste in Task 4.
- Stop pe un run de brainstorming: cardul ramane "stopped" si Reconcile nu-l reporneste — test in Task 5.

---

### Task 1: Store — tabelele si operatiile

**Files:**
- Create: `internal/store/migrations/038_brainstorm.sql`
- Create: `internal/store/brainstorm.go`
- Modify: `internal/store/accounts.go` (`SetProjectAccount`: sterge si sesiunile de brainstorming ale proiectului, in aceeasi tranzactie)
- Modify: `internal/config/config.go` (`ProjectBrainstormDir`)
- Test: `internal/store/brainstorm_test.go`

**Interfaces:**
- Produces (store):
  - Constante: `CardWaiting="waiting"`, `CardRunning="running"`, `CardDone="done"`, `CardStopped="stopped"`, `CardFailed="failed"`, `CardSkipped="skipped"`; `CardIdea="idea"`, `CardWild="wild"`, `CardQuestion="question"`.
  - `type BrainstormTopic struct { ID, ProjectID int64; Name, Description string; Archived bool; Participants []int64; CreatedAt time.Time }` (json camelCase).
  - `type BrainstormRound struct { ID, TopicID int64; Text string; CreatedAt time.Time }`
  - `type BrainstormCard struct { ID, TopicID, RoundID, AgentID int64; Status, Kind, Title, Text, Basis string; Options []string; Answer string; Picked, Priority bool; FollowsID *int64; RunID, Error string; CreatedAt, UpdatedAt time.Time }`
  - `type BrainstormFile struct { ID, TopicID int64; Name, Stored, Mime string; Size int64; CreatedAt time.Time }`
  - `CreateTopic(t *BrainstormTopic) error` (ErrConflict pe nume duplicat in proiect), `UpdateTopic(t BrainstormTopic) error` (inlocuieste participantii), `GetTopic(id int64) (BrainstormTopic, error)`, `ListTopics(projectID int64) ([]BrainstormTopic, error)`
  - `AddRound(r *BrainstormRound, agentIDs []int64) ([]BrainstormCard, error)` — runda + cate un card waiting per agent, intr-o tranzactie
  - `AddCard(c *BrainstormCard) error`, `GetCard(id int64) (BrainstormCard, error)`, `TopicRounds(topicID int64) ([]BrainstormRound, error)`, `TopicCards(topicID int64) ([]BrainstormCard, error)` (ordonate dupa id)
  - `NextCard(agentID int64, priorityOnly bool) (BrainstormCard, bool, error)` — waiting, `priority DESC, id`
  - `SetCardState(id int64, status, runID, errText string) error`
  - `FinishCard(id int64, kind, title, text, basis string, options []string) error` — status done; ErrConflict daca e deja done
  - `AnswerCard(id int64, answer string) error` — doar question done fara raspuns; altfel ErrConflict
  - `SetCardPicked(id int64, picked bool) error`, `SetCardPriority(id int64, on bool) error`
  - `QuestionsAsked(roundID, agentID int64) (int, error)`
  - `OpenQuestions(projectID int64) (map[int64]int, error)` — topicID -> intrebari fara raspuns
  - `SkipOrphanCards(projectID int64) (int64, error)` — waiting/stopped/failed ale agentilor care nu mai sunt participanti sau sunt stersi -> skipped
  - `RequeueRunningCards() error` — running -> waiting (Recover)
  - `BrainstormSession(topicID, agentID int64) (sessionID, hash string, err error)`, `SetBrainstormSession(topicID, agentID int64, sessionID, hash string) error`
  - `AddTopicFile(f *BrainstormFile) error`, `ListTopicFiles(topicID int64) ([]BrainstormFile, error)`, `GetTopicFile(id int64) (BrainstormFile, error)`, `DeleteTopicFile(id int64) error`
  - `FindProjectFile(projectID int64, name string) (Attachment, error)` si `SetAttachmentSize(id, size int64) error` (pentru "Save to Files" care suprascrie)
- Produces (config): `ProjectBrainstormDir(root string, projectID int64) string` = `<root>/<pid>/brainstorm` (fisierele unui subiect in `<dir>/<topicID>/`).

- [ ] **Step 1: Write the failing tests** in `internal/store/brainstorm_test.go`:
  - `TestBrainstormTopicNameUnique`: doua `CreateTopic` cu acelasi nume in acelasi proiect -> a doua `errors.Is(err, ErrConflict)`; acelasi nume in alt proiect merge.
  - `TestBrainstormRoundCards`: topic cu 2 participanti, `AddRound` -> 2 carduri `CardWaiting`; `NextCard(a, false)` intoarce cardul lui `a`; dupa `SetCardPriority` pe un card mai nou, `NextCard(a, true)` intoarce cardul prioritar.
  - `TestBrainstormAnswerOnce`: `FinishCard(q, CardQuestion, ...)`, `AnswerCard(q, "Yes")` merge, al doilea `AnswerCard` -> ErrConflict; `OpenQuestions(pid)[topic] == 0` dupa raspuns, `1` inainte.
  - `TestBrainstormSkipOrphans`: card waiting al unui agent scos din `Participants` -> dupa `SkipOrphanCards` status `CardSkipped`; card al unui participant ramane waiting.
  - `TestBrainstormSessionsResetOnAccount`: `SetBrainstormSession`, apoi `SetProjectAccount` cu alt cont -> `BrainstormSession` intoarce sessionID gol.
- [ ] **Step 2: Run** `go test ./internal/store -run Brainstorm` — Expected: FAIL (tipurile nu exista).
- [ ] **Step 3: Write `038_brainstorm.sql`**: tabelele `brainstorm_topics` (UNIQUE(project_id, name), FK projects ON DELETE CASCADE), `brainstorm_participants` (PK topic_id, agent_id), `brainstorm_rounds`, `brainstorm_cards` (options ca JSON text, follows_id NULL), `brainstorm_files`, `brainstorm_sessions` (PK topic_id, agent_id). Indici pe `brainstorm_cards(agent_id, status)` si `(topic_id)`.
- [ ] **Step 4: Implement** functiile din Interfaces in `internal/store/brainstorm.go` (stilul din `library.go`: `jsonList`, `mustAffect`, `notFound`), plus modificarea din `SetProjectAccount` si `config.ProjectBrainstormDir`.
- [ ] **Step 5: Run** `go test ./internal/store` — Expected: PASS.
- [ ] **Step 6: Commit** `git commit -m "Brainstorm: store tables and queries"`

### Task 2: MCP — tool-ul brainstorm_reply

**Files:**
- Modify: `internal/mcp/server.go` (Caller, input, Backend, dispatch)
- Modify: `internal/mcp/tools.go` (tool + `toolsFor`)
- Test: `internal/mcp/server_test.go` (fakeBackend primeste metoda)

**Interfaces:**
- Produces:
  - `Caller.BrainstormCardID int64` — != 0 = run de brainstorming; `toolsFor` intoarce doar `[]Tool{brainstormReplyTool}` (ca la DraftID).
  - `type BrainstormReplyInput struct { Kind, Title, Text, Basis string; Options []string }`
  - `Backend.BrainstormReply(c Caller, in BrainstormReplyInput) (string, error)`
  - Tool `brainstorm_reply`, schema: `kind` (idea | wild | question, obligatoriu), `title` (obligatoriu, max 120), `text` (obligatoriu: ideea sau intrebarea), `basis` (pe ce se bazeaza: fisiere, atasamente, ce a gasit in proiect), `options` (array, doar la question, 2-5). Descrierea in engleza spune: un singur apel pe run, "wild" doar pentru ideile cu adevarat indraznete, "question" doar cand raspunsul schimba ideea.

- [ ] **Step 1: Write the failing test** `TestBrainstormToolsOnly`: `names(Caller{BrainstormCardID: 5, IsLead: true, CanAddAgents: true})` == `"brainstorm_reply"`; un `tools/call` cu `{"kind":"question","title":"T","text":"Q?","options":["a","b"]}` ajunge in fakeBackend cu `Options == []string{"a","b"}`.
- [ ] **Step 2: Run** `go test ./internal/mcp -run Brainstorm` — Expected: FAIL.
- [ ] **Step 3: Implement** campul, tipul, metoda in interfata, tool-ul, ramura in `toolsFor` si cazul `"brainstorm_reply"` din dispatch (`argList` pentru options). `team.Team` nu compileaza inca: adauga in Task 4 metoda; pana atunci `go build ./internal/mcp` e verificarea.
- [ ] **Step 4: Run** `go test ./internal/mcp` — Expected: PASS.
- [ ] **Step 5: Commit** impreuna cu Task 4 (interfata Backend cere implementarea din team ca `go build ./...` sa treaca); daca rulezi task-urile separat, adauga in acest task un stub `BrainstormReply` in `internal/team/brainstorm.go` care intoarce `errors.New("not available")`.

### Task 3: Prompt — tura de brainstorming

**Files:**
- Create: `internal/prompt/brainstorm.go`
- Test: `internal/prompt/brainstorm_test.go`

**Interfaces:**
- Produces:
  - `type BrainstormIdea struct { Author, Kind, Title, Text, Basis string }`
  - `type BrainstormQA struct { Author, Question, Answer string }`
  - `type BrainstormContext struct { Topic, Description, Message string; Files []FileRef; Picked []BrainstormIdea; Unpicked []string; Own []BrainstormIdea; QA []BrainstormQA; QuestionsLeft int; Another bool }`
  - `func BrainstormTurn(c BrainstormContext) string`

Continutul turei (engleza), in ordine: ca e o sesiune de brainstorming pe subiectul X, nu munca (nu modifica fisiere, nu porneste task-uri, poate citi codul si documentele); descrierea subiectului; atasamentele (nume + cale, inclusiv poze, "read them"); ideile alese (complet); titlurile nealese ("already proposed, not picked: do not repeat them"); ce a propus el insusi; intrebarile si raspunsurile; mesajul curent. Apoi regulile: porneste de la proiect si atasamente, raspunde din rolul tau, spune pe ce te bazezi in `basis`, intreaba doar daca raspunsul schimba ideea, foloseste `wild` doar pentru idei indraznete, raspunde cu exact un apel `brainstorm_reply`. Cu `QuestionsLeft == 0`: "Do not ask more questions; propose with what you know." Cu `Another`: "Give a different idea from the ones you already gave."

- [ ] **Step 1: Write the failing test** `TestBrainstormTurn`: tura contine numele subiectului, descrierea, mesajul, calea unui fisier din `Files`, titlul unei idei din `Picked`, `"brainstorm_reply"`; cu `QuestionsLeft: 0` contine `"Do not ask more questions"`, cu `QuestionsLeft: 2` nu; cu `Another: true` contine `"different idea"`.
- [ ] **Step 2: Run** `go test ./internal/prompt -run Brainstorm` — Expected: FAIL.
- [ ] **Step 3: Implement** `BrainstormTurn` (folosind `FilesBlock` daca se potriveste formatului).
- [ ] **Step 4: Run** `go test ./internal/prompt` — Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "Brainstorm: agent turn"`

### Task 4: Team — run-ul de brainstorming (pornire, raspuns, final)

**Files:**
- Create: `internal/team/brainstorm.go`
- Modify: `internal/team/run.go` (`startRun` -> `startRunOpts`; `recordOutcome` ramura pentru card)
- Modify: `internal/team/team.go` (`activeRun.cardID`, `activeRun.topicID`)
- Test: `internal/team/brainstorm_test.go`

**Interfaces:**
- Consumes: Task 1 (store), Task 2 (`mcp.Caller.BrainstormCardID`, `mcp.BrainstormReplyInput`), Task 3 (`prompt.BrainstormTurn`).
- Produces:
  - `type runOpts struct { TaskID int64; Turn string; Card *store.BrainstormCard }`; `startRun(p, a, agents, taskID, turn)` ramane si cheama `startRunOpts(p, a, agents, runOpts{TaskID: taskID, Turn: turn})`.
  - `func (t *Team) brainstormRun(p store.Project, a store.Agent, agents []store.Agent, card store.BrainstormCard) error` — construieste `prompt.BrainstormContext` din `TopicCards`/`TopicRounds`/`ListTopicFiles` (vizibilitate: ideile alese ale tuturor; titlurile nealese din rundele anterioare; ce a dat agentul; intrebarile+raspunsurile din rundele anterioare ale tuturor si din runda curenta doar ale lui; `QuestionsLeft = 2 - QuestionsAsked`; `Another` = cardul are un frate idea/wild al aceluiasi agent in aceeasi runda), pune cardul `running` si porneste run-ul. Se cheama cu `t.work` tinut.
  - `func (t *Team) BrainstormReply(c mcp.Caller, in mcp.BrainstormReplyInput) (string, error)`
  - Eveniment SSE: `t.emit(projectID, "brainstorm", map[string]int64{"topicId": id})` la orice schimbare de card.

Comportament in `startRunOpts` cu `Card != nil`: tool-uri `prompt.ReadOnlyTools(a.Tools)` (fara linia `prompt.ReadOnlyTurn`); system prompt-ul obisnuit al agentului; hash = `prompt.Hash(home.Path + "\n" + system + "\nbrainstorm")`; resume din `BrainstormSession(topic, agent)` (nu din `a.SessionID`); `--add-dir` si pe `ProjectBrainstormDir`; Caller doar cu `ProjectID, AgentID, RunID, BrainstormCardID` (fara flag-uri de lider); `activeRun{cardID, topicID}`.

`BrainstormReply`: refuza daca cardul nu e `running` pe acest run ("You already answered this card."); kind in idea|wild|question; title 1-120 caractere, text 1-4000, basis max 500; question cere 2-5 optiuni de max 80 si `QuestionsAsked < 2` (altfel "You already asked 2 questions in this round; propose an idea with what you know."); apoi `FinishCard` + emit.

`recordOutcome` cu `ar.cardID != 0`: `FinishRun` + tokeni ca acum; sesiunea in `SetBrainstormSession` (nu `SetSession`); fara mesaj in chat si fara nota "Run failed" in chat; cardul: deja done -> nimic; `RunStopped` -> `CardStopped`; `RunFailed` cu `runner.IsAuthError` -> `CardWaiting` + acelasi drum de pauza ca `authFailed` (verifica ca merge cu taskID 0); alt `RunFailed` -> `CardFailed` cu eroarea; done cu `o.Result` negol -> `FinishCard(CardIdea, primul rand taiat la 120, tot textul, "")`; done gol -> `CardFailed` "The agent gave no answer.".

- [ ] **Step 1: Write the failing tests** (fixture-ul `setup` din `fake_test.go`; un helper `topicWith(t, f, agents...) store.BrainstormTopic`):
  - `TestBrainstormRunIsReadOnlyAndSeparate`: dupa `AddRound` si `brainstormRun` pentru Rook: spec-ul ultimului run nu contine `--resume` chiar daca `f.rook.SessionID` e setat; `--settings` nu contine `Write`; tokenul rezolvat are `BrainstormCardID == card.ID`; cardul e `running`.
  - `TestBrainstormReplyOnce`: `BrainstormReply` cu idea -> card done, Kind idea; al doilea apel -> eroare "already answered".
  - `TestBrainstormQuestionLimit`: doua carduri question deja in runda pentru agent -> `BrainstormReply` cu question -> eroare care contine "2 questions".
  - `TestBrainstormFallbackToResult`: run terminat (`f.fs.finish`) cu `Result: "Use rollback netcode\nbecause..."` fara reply -> card done, Kind idea, Title "Use rollback netcode"; sesiunea din Outcome ajunge in `BrainstormSession`, nu in `agents.session_id`; niciun mesaj nou in chat-ul agentului.
  - `TestBrainstormStoppedRun`: finish cu `Status: store.RunStopped` -> `CardStopped`.
- [ ] **Step 2: Run** `go test ./internal/team -run Brainstorm` — Expected: FAIL.
- [ ] **Step 3: Implement** `runOpts`/`startRunOpts`, campurile din `activeRun`, `brainstormRun`, `BrainstormReply`, ramura din `recordOutcome`.
- [ ] **Step 4: Run** `go test ./internal/team ./internal/mcp && go vet ./...` — Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "Brainstorm: read-only runs with their own session and brainstorm_reply"`

### Task 5: Team — programarea si actiunile utilizatorului

**Files:**
- Modify: `internal/team/reconcile.go` (pasii de brainstorming, `Recover`)
- Modify: `internal/team/brainstorm.go` (actiunile)
- Modify: `internal/team/stop.go` (Stop pe card)
- Test: `internal/team/brainstorm_test.go`

**Interfaces:**
- Consumes: Task 4 (`brainstormRun`, `activeRun.cardID`).
- Produces (toate iau `t.work`, emit "brainstorm", apoi `Kick`):
  - `NewRound(topicID int64, text string) (store.BrainstormRound, []store.BrainstormCard, error)` — text 1-4000; topic nearhivat; carduri pentru participantii existenti (nesterși)
  - `AnswerQuestion(cardID int64, answer string) error` — `AnswerCard` + card nou waiting, acelasi agent si runda, `FollowsID = cardID`
  - `AnotherIdea(cardID int64) error` — doar pe idea/wild done; card nou waiting, aceeasi runda si agent
  - `RunNow(cardID int64) error` — doar waiting; `SetCardPriority(true)`
  - `StopCard(cardID int64) error` — running: `stopRun` pe `ar.cardID == cardID` cu userStop (finalul il face `CardStopped`); waiting: direct `CardStopped`
  - `RetryCard(cardID int64) error` — stopped/failed -> waiting
  - `PickCard(cardID int64, picked bool) error` — doar idea/wild done
  - `SendToLead(topicID, leadID int64) (store.Message, error)` — leadID trebuie sa fie lider sau radacina in proiect; text: "From the brainstorm \"<topic>\": turn these picked ideas into tasks." + lista ideilor alese (titlu, autor, text); prin `UserMessage`, deci ajunge ca cerere din chat. Fara idei alese: eroare "Pick at least one idea first.".
  - `BrainstormMarkdown(topicID int64) (name string, body []byte, err error)` — name `brainstorm-<slug>.md`; continut: titlu, descriere, ideile alese (titlu, autor, wild marcat, text, basis), intrebari + raspunsuri.

Reconcile (cu "skip" = `t.isActive(a.ID) || full(a)`):
1. La inceput (dupa citirea agentilor): `SkipOrphanCards(projectID)`.
2. Dupa pasul 0 (decizii): pentru fiecare agent ne-skip cu `NextCard(a, true)`: daca are note nelivrate -> `notifyRun`; altfel `brainstormRun`.
3. Dupa pasul 2 (note): pentru fiecare agent ne-skip, fara task Queued nestopat asignat lui (`hasQueued`) si fara note nelivrate: `NextCard(a, false)` -> `brainstormRun`.
4. Ramura de pauza: dupa notele utilizatorului, pentru fiecare agent inactiv fara note nelivrate: `NextCard(a, false)` -> `brainstormRun` (fara `full`).
Agentii detasati participa (nu se filtreaza dupa `Detached`). `Recover`: `RequeueRunningCards()`.

- [ ] **Step 1: Write the failing tests**:
  - `TestBrainstormWaitsForQueuedTask`: Rook are un task Queued si un card waiting -> dupa `Reconcile` ruleaza task-ul (spec-ul nu are `BrainstormCardID`), cardul ramane waiting; dupa finish-ul run-ului pe task (task trecut in review) -> urmatorul `Reconcile` porneste cardul.
  - `TestBrainstormRunNowBeforeQueue`: Rook ruleaza un task si mai are unul Queued; `RunNow(card)`; la finish -> urmatorul run e al cardului, nu task-ul din coada.
  - `TestBrainstormOnPausedTeam`: proiect pe pauza, card waiting -> `Reconcile` porneste cardul.
  - `TestBrainstormStopStays`: card running, `StopCard`, finish cu `RunStopped` -> `CardStopped`; `Reconcile` nu porneste niciun run nou pentru Rook; `RetryCard` -> urmatorul `Reconcile` il porneste.
  - `TestBrainstormRemovedParticipantSkipped`: card waiting al Verei, Vera scoasa din participanti (`UpdateTopic`) -> dupa `Reconcile` cardul e `CardSkipped`, niciun run pentru Vera.
  - `TestBrainstormRecoverRequeues`: card running, `Recover()` -> waiting.
  - `TestBrainstormAnswerFollowUp`: card question done, `AnswerQuestion("Co-op")` -> card nou waiting cu `FollowsID`, `OpenQuestions` scade la 0.
  - `TestBrainstormSendToLead`: doua idei alese -> `SendToLead(topic, lead)` adauga un mesaj al utilizatorului catre lead care contine ambele titluri; `SendToLead` cu un worker fara echipa -> eroare; fara idei alese -> eroare "Pick at least one idea".
- [ ] **Step 2: Run** `go test ./internal/team -run Brainstorm` — Expected: FAIL.
- [ ] **Step 3: Implement** pasii din Reconcile, `Recover`, actiunile si `BrainstormMarkdown`.
- [ ] **Step 4: Run** `go test -race ./internal/team` — Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "Brainstorm: scheduling when agents are free, Run now, stop, answers, send to lead"`

### Task 6: Web — API-ul

**Files:**
- Create: `internal/web/api_brainstorm.go`
- Modify: `internal/web/server.go` (rutele)
- Test: `internal/web/brainstorm_test.go`

**Interfaces:**
- Consumes: Task 1 (store), Task 5 (actiunile din team).
- Produces (JSON camelCase):
  - `GET /api/projects/{id}/brainstorm` -> `{topics: [{id, name, description, archived, participants, openQuestions, waiting}], openQuestions: N}`
  - `POST /api/projects/{id}/brainstorm` `{name, description, participants: [ids]}` -> 201 topic (nume 1-60, descriere max 4000, cel putin un participant din proiect; 409 pe nume duplicat)
  - `GET /api/brainstorm/{tid}` -> `{topic, files, rounds: [{id, text, createdAt, cards: [card + agentName + busy: "T-42" | "" ]}]}`; `busy` vine din `team.AgentStatus` cand cardul e waiting si agentul lucreaza
  - `PATCH /api/brainstorm/{tid}` `{name?, description?, participants?, archived?}`
  - `POST /api/brainstorm/{tid}/rounds` multipart: `text` + `files` (fisierele se adauga intai la subiect, apoi runda) -> 201 `{round, cards}`
  - `POST /api/brainstorm/{tid}/files` multipart (`readUploads`, max 50 pe subiect -> 400), `GET /api/brainstorm/{tid}/files/{fid}` (prin `serveStored`), `DELETE /api/brainstorm/{tid}/files/{fid}`; pe disc in `ProjectBrainstormDir/<tid>/`, staging + rename ca la `taskfiles.Save`
  - `POST /api/brainstorm/cards/{id}/{pick|unpick|answer|another|run-now|stop|retry}` (`answer` cu `{answer}`) -> 204; stare gresita -> 409
  - `POST /api/brainstorm/{tid}/save` -> `{name}`: `BrainstormMarkdown`; daca exista un fisier de proiect cu acelasi nume (`FindProjectFile`) il suprascrie pe disc si actualizeaza marimea, altfel il adauga ca `KindProjectFile`; publica evenimentul de proiect ca Files sa se reimprospateze
  - `POST /api/brainstorm/{tid}/send` `{leadId}` -> `{messageId}`

- [ ] **Step 1: Write the failing tests** (`setup(t)` din `server_test.go`):
  - `TestBrainstormTopicAPI`: create -> 201; acelasi nume -> 409; participant din alt proiect -> 400; `GET` lista are topic-ul cu `openQuestions: 0`.
  - `TestBrainstormRoundWithFiles`: multipart cu `text` si o poza -> 201 cu cate un card waiting per participant; `GET /api/brainstorm/{tid}` are fisierul in `files` si runda cu cardurile; fisierul exista sub `ProjectBrainstormDir(<root>, pid)/<tid>/`.
  - `TestBrainstormSaveOverwrites`: doua `save` consecutive -> un singur fisier de proiect `brainstorm-<slug>.md`, cu continutul ultimei salvari (contine titlul unei idei alese).
  - `TestBrainstormCardConflicts`: `answer` pe un card idea -> 409; `pick` pe un card waiting -> 409.
- [ ] **Step 2: Run** `go test ./internal/web -run Brainstorm` — Expected: FAIL.
- [ ] **Step 3: Implement** handler-ele si rutele.
- [ ] **Step 4: Run** `go test ./internal/web && go vet ./...` — Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "Brainstorm: API"`

### Task 7: UI — tab-ul Brainstorm

**Files:**
- Create: `internal/web/static/js/brainstorm.js`
- Modify: `internal/web/static/js/workspace.js` (TABS: `{ id: "brainstorm", label: "Brainstorm" }` intre Files si Stats; ruta `#/p/<pid>/brainstorm/<tid>` in `parseWorkspaceRoute`; insigna; evenimentul SSE)
- Modify: `internal/web/static/app.css` (clasele `.bs-*`)

**Interfaces:**
- Consumes: API-ul din Task 6.
- Produces: `export function brainstormView(projectId, { agents, onBadge }) -> { el, open(topicId), refresh() }`.

Layout F din schite: stanga lista subiectelor (`# name`, insigna cu intrebari deschise, "+ New topic" deschide un formular: nume, descriere, participanti cu avatare `ui.toneOf`); dreapta: capul subiectului (descriere, participanti, Edit, atasamente cu Upload/drop/Cmd+V prin `paste.js` `onPasteFiles`, actiunile "Save to Files" si "Send to lead" cu alegerea liderului, implicit prima radacina), zona "Picked" fixata sus, apoi rundele: mesajul utilizatorului aliniat dreapta (`md.userText`), sub el un rand de carduri. Card: avatar + nume; waiting: "Waiting, busy on T-42" / "Waiting" + "Run now" + "Stop"; running: "Thinking…" + "Stop"; idea: titlu, text (`md`), "Based on: …", Pick/Unpick, "Another idea"; wild: chenar punctat mov + eticheta "wild"; question: textul, butoane pentru optiuni + "Other" (camp text), apoi raspunsul afisat; stopped/failed: motivul + "Retry"; skipped: estompat. Campul de jos: textarea care creste (ca in chat) + Attach + Send. Evenimentul SSE "brainstorm" cu `topicId` egal cu cel deschis -> `refresh()`; orice "brainstorm" -> reincarca lista (insigna).

- [ ] **Step 1: Implement** `brainstorm.js` si legaturile din `workspace.js`/`app.css`.
- [ ] **Step 2: Verify syntax**: pentru `brainstorm.js` si `workspace.js`, copiaza ca `.mjs` in scratchpad si `node --check` — Expected: fara erori.
- [ ] **Step 3: Verify in browser** (memoria proiectului: screenshot cu Chrome headless prin CDP): ruleaza un binar de test separat, cu datele intr-un folder temporar si pe alt port: `go build -o <scratch>/hobstack-test ./cmd/hobstack && HOME=<scratch>/home <scratch>/hobstack-test start --port 7788 --no-open` (instanta utilizatorului de pe 7777 nu se atinge), creeaza un proiect cu doi agenti, un subiect si o runda; screenshot la 1440x900 si 390x844: lista de subiecte, cardurile waiting, formularul. Expected: fara suprapuneri, fara scroll orizontal pe telefon.
- [ ] **Step 4: Commit** `git commit -m "Brainstorm: tab with topics, rounds and cards"`

### Task 8: Documentatie

**Files:**
- Modify: `CLAUDE.md` (paragraf "Gata (Brainstorm, spec in docs/superpowers/specs/2026-11-11-hobstack-brainstorm-design.md): ..." in stilul celorlalte: tabelele, migrarea 038, tool-ul brainstorm_reply, programarea in Reconcile, Run now, API, brainstorm.js)

- [ ] **Step 1: Write** paragraful.
- [ ] **Step 2: Run** toate verificarile: `go vet ./... && go build -race ./... && GOOS=windows go build -o /dev/null ./cmd/hobstack && go test ./...` — Expected: PASS.
- [ ] **Step 3: Commit** `git commit -m "Brainstorm: docs"`
