# Brainstorm: idei combinate si tile-uri — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Utilizatorul combina doua idei de brainstorming intr-una (scrisa de un agent, editabila apoi), iar cardurile unei runde devin tile-uri egale, usor de citit, cu detaliile intr-o modala.

**Architecture:** Cardul combinat e un `brainstorm_cards` obisnuit cu doua surse (`source_a`, `source_b`), pus in runda sursei mai noi; ruleaza prin acelasi Reconcile / startRunOpts / brainstorm_reply, dar cu prompt propriu, fara sesiune reluata si fara intrebari. "Merged" se calculeaza la citire. UI-ul (brainstorm.js) deseneaza rundele ca grid de tile-uri, un rand "Combined" dupa runda sursei mai noi, o modala per idee si bara de combinare.

**Tech Stack:** Go, SQLite (modernc), vanilla JS embedded, SSE.

**Spec:** docs/superpowers/specs/2026-11-12-hobstack-brainstorm-combine-design.md (extinde docs/superpowers/specs/2026-11-11-hobstack-brainstorm-design.md). Schita aleasa: .superpowers/brainstorm/46333-1794439036/content/cards-variants.html, varianta B.

## Global Constraints

- `go vet ./...` si `go build -race ./...` trec; cross-compile `GOOS=darwin GOARCH=arm64 go build ./cmd/hobstack` si `GOOS=windows go build ./cmd/hobstack`.
- JS: copiaza fisierul ca .mjs si `node --check` (pe .js nu prinde erorile de sintaxa).
- Tot ce vede utilizatorul si prompturile: engleza. Comentariile din cod: romana.
- Tema existenta din internal/web/static/app.css (tokenii `--card`, `--line-2`, `--amber*`, `--teal*`, `--purple`, Figtree). Fara dependente noi.
- Limitele textului ideii raman ale brainstorm_reply: titlu 1-120, text 1-4000 caractere (si la Edit).
- Commit-uri doar locale pe branch-ul de lucru; nu se face push (push-ul doar pe remote-ul `hobstack`, la cererea utilizatorului).

## Review Focus

1. O sursa stearsa / agent sters / sursa care nu mai e idee terminata cand se apasa Combine: 409 cu mesaj clar, nu card orfan.
2. Cardul combinat nu trebuie sa scrie sau sa reia sesiunea (subiect, agent): un run de combinare urmat de o runda normala reia sesiunea veche neatinsa (test in Task 3).
3. brainstorm_reply cu kind question pe un card combinat: refuzat cu mesaj, cardul ramane running (agentul mai poate raspunde).
4. Un rand "Combined" intre runde cand exista runde mai noi decat sursele; cardul combinat nu apare si in gridul rundei lui (test in Task 4 pe forma JSON).
5. Tile-uri cu titlu de 120 caractere / text de 4000 / fara text (waiting): aceeasi inaltime pe rand, nimic nu iese din tile (verificat cu captura in Task 5).

---

### Task 1: Store — sursele, Edited, Merged

**Files:**
- Create: `internal/store/migrations/039_brainstorm_combine.sql`
- Modify: `internal/store/brainstorm.go`
- Test: `internal/store/brainstorm_test.go`

**Interfaces:**
- Produces:
  - `BrainstormCard.SourceA, SourceB *int64` (json `sourceA`, `sourceB`; nil pe cardurile normale) si `Edited bool` (json `edited`).
  - `func (s *Store) EditCard(id int64, title, text string) error` — doar card `done` cu `source_a` nenul; altfel `ErrConflict`; pune `edited = 1`, `updated_at`.
  - `func MergedSources(cards []BrainstormCard) map[int64]int64` — sursa -> id-ul celui mai nou card combinat cu status done / waiting / running care o foloseste.
  - `func (c BrainstormCard) Combined() bool` — `c.SourceA != nil`.

- [ ] **Step 1: Migrarea 039**: `ALTER TABLE brainstorm_cards ADD COLUMN source_a INTEGER;` `source_b INTEGER;` `edited INTEGER NOT NULL DEFAULT 0;` (fara FK: sursele raman ca istoric; cardurile nu se sterg individual).
- [ ] **Step 2: Teste care pica**:
  - `TestAddCardWithSources`: AddCard cu SourceA=&a, SourceB=&b -> GetCard intoarce aceleasi pointere si `Combined()==true`; un card fara surse intoarce nil/nil.
  - `TestEditCard`: pe card combinat done -> title/text schimbate, `Edited==true`; pe card combinat waiting -> `ErrConflict`; pe card normal done -> `ErrConflict`.
  - `TestMergedSources`: carduri a,b (done), c=combinat(a,b) done, d=combinat(a,b) failed, e=combinat(c,x) waiting -> map {a:c, b:c, c:e, x:e}; daca c devine failed -> a,b lipsesc din map.
- [ ] **Step 3:** `go test ./internal/store -run 'AddCardWithSources|EditCard|MergedSources'` -> FAIL.
- [ ] **Step 4: Implementare**: coloanele in `brainstormCardCols`, `scanCard` (cu `nullPtr` la insert, ca `follows_id`), `AddCard`, `EditCard` (UPDATE ... WHERE id=? AND status='done' AND source_a IS NOT NULL; 0 randuri -> ErrConflict), `MergedSources` (pura; "cel mai nou" = id mai mare).
- [ ] **Step 5:** testele trec, plus `go test ./internal/store`.
- [ ] **Step 6: Commit** `Brainstorm store: combined cards (sources, edited, merged)`.

### Task 2: Prompt — tura de combinare si eticheta in context

**Files:**
- Modify: `internal/prompt/brainstorm.go`
- Test: `internal/prompt/brainstorm_test.go`

**Interfaces:**
- Produces:
  - `BrainstormIdea.CombinedFrom string` — "A + B" (titlurile surselor); `writeIdea` adauga `(combined from A + B)` dupa titlu cand nu e gol.
  - `type BrainstormCombine struct { Topic, Description string; Files []FileRef; Picked []BrainstormIdea; A, B BrainstormIdea }`
  - `func BrainstormCombineTurn(c BrainstormCombine) string`

- [ ] **Step 1: Teste care pica**:
  - `TestCombineTurn`: rezultatul contine numele subiectului, descrierea, ambele idei complete (titlu, autor, text, `Basis:`), "one coherent idea", "what you left out", "Do not ask questions", "exactly one brainstorm_reply call"; NU contine "Current message from the user" si nici "different idea".
  - `TestWriteIdeaCombinedFrom`: o idee aleasa cu CombinedFrom "Host + Async" apare ca `- Title (combined from Host + Async) (Dan)`.
- [ ] **Step 2:** `go test ./internal/prompt -run 'CombineTurn|CombinedFrom'` -> FAIL.
- [ ] **Step 3: Implementare**: aceleasi blocuri ca BrainstormTurn pentru subiect / atasamente / Picked (extrage-le intr-un helper comun daca se repeta), apoi "Combine these two ideas into one:" cu A si B (writeIdea cu autor), apoi regulile: "Write one coherent idea that keeps the best of both and resolves where they conflict; it is not the two texts one after the other.", "Say briefly in the text what you left out and why.", "Say in basis what you rely on.", "Use kind wild only if the combined idea is truly bold.", "Do not ask questions.", "Reply with exactly one brainstorm_reply call."
- [ ] **Step 4:** testele trec, `go test ./internal/prompt`.
- [ ] **Step 5: Commit** `Brainstorm prompt: combine turn`.

### Task 3: Team — Combine, run fara sesiune, Edit, reguli

**Files:**
- Modify: `internal/team/brainstorm.go`, `internal/team/run.go`
- Test: `internal/team/brainstorm_test.go`

**Interfaces:**
- Consumes: Task 1 (`SourceA/SourceB`, `EditCard`, `Combined()`), Task 2 (`BrainstormCombineTurn`, `CombinedFrom`).
- Produces:
  - `func (t *Team) Combine(topicID, aID, bID, agentID int64) (store.BrainstormCard, error)`
  - `func (t *Team) EditCombined(cardID int64, title, text string) error`

- [ ] **Step 1: Teste care pica** (in stilul testelor existente din fisier, cu runner-ul fals):
  - `TestCombineCreatesWaitingCard`: doua idei done din runde diferite, agent participant -> card waiting cu SourceA/SourceB, RoundID = runda mai noua, AgentID = agentul ales.
  - `TestCombineRejects`: aceeasi idee de doua ori, o intrebare, o idee waiting, carduri din subiecte diferite, subiect arhivat -> `CardStateError`; agent care nu e participant -> `BrainstormError`.
  - `TestCombineRunNoSession`: subiectul are sesiune salvata S pentru agent; run-ul cardului combinat porneste fara `--resume` (Spec fara sesiune), turn-ul vine din BrainstormCombineTurn, iar dupa final `BrainstormSession` e tot S.
  - `TestCombineReplyNoQuestion`: brainstorm_reply cu kind question pe cardul combinat -> eroare "Do not ask questions; combine the two ideas.", cardul ramane running; cu kind idea -> done.
  - `TestAnotherIdeaOnCombined`: AnotherIdea pe card combinat -> `CardStateError`.
  - `TestEditCombined`: titlu/text noi -> salvate, Edited; titlu gol sau >120, text >4000 -> `BrainstormError`; pe card normal -> `CardStateError`.
  - `TestContextShowsCombinedFrom`: o idee combinata aleasa apare in tura normala urmatoare cu "(combined from <A> + <B>)"; markdown-ul (BrainstormMarkdown) si SendToLead au linia "Combined from: <A> + <B>".
- [ ] **Step 2:** `go test ./internal/team -run 'Combine|EditCombined|AnotherIdeaOnCombined|ContextShowsCombinedFrom'` -> FAIL.
- [ ] **Step 3: Implementare**:
  - `Combine`: sub `t.work`, GetCard pe ambele, ambele cu TopicID == topicID, `isIdea` pe ambele, id-uri diferite, subiect nearhivat; agentul in `topic.Participants` si in ListAgents (nesters); `AddCard` cu RoundID = max(RoundID), SourceA = prima, SourceB = a doua; emit "brainstorm" + Kick (ca `cardAction`).
  - `brainstormTurn`: daca `card.Combined()`, construieste `BrainstormCombine` (subiect, atasamente, Picked fara cele doua surse, A/B cu autor) si intoarce `BrainstormCombineTurn`. In ramura normala seteaza `CombinedFrom` pe ideile combinate (titlurile surselor din `cards`).
  - `run.go`: pentru `card.Combined()` nu citi `BrainstormSession` (sessionID gol -> fara resume).
  - `finishCard`: citeste cardul inainte de `SetBrainstormSession` si sare salvarea pentru cardurile combinate.
  - `BrainstormReply`: pe card combinat, kind question -> eroarea de mai sus.
  - `AnotherIdea`: refuza cardurile combinate ("Another idea does not work on a combined idea.").
  - `EditCombined`: trim, limitele (120 / 4000, ambele obligatorii), `cardAction(cardID, false, ...)` -> `EditCard`; ErrConflict -> `CardStateError("Only a finished combined idea can be edited.")`.
  - `BrainstormMarkdown` / `sendToLeadLocked`: pentru ideile combinate adauga "Combined from: A + B".
- [ ] **Step 4:** testele trec, `go test ./internal/team`, `go vet ./...`.
- [ ] **Step 5: Commit** `Brainstorm: combine two ideas into one`.

### Task 4: API — combine, edit, forma subiectului

**Files:**
- Modify: `internal/web/api_brainstorm.go`, `internal/web/server.go`
- Test: `internal/web/brainstorm_test.go`

**Interfaces:**
- Consumes: Task 3 (`Combine`, `EditCombined`), Task 1 (`MergedSources`).
- Produces (JSON pentru Task 5-6):
  - `POST /api/brainstorm/{id}/combine` body `{"a":<cardId>,"b":<cardId>,"agentId":<id>}` -> 201 cu cardul; erorile prin `fail` (BrainstormError 400, CardStateError 409, ca acum).
  - `POST /api/brainstorm/cards/{id}/edit` body `{"title":"","text":""}` -> 204.
  - `GET /api/brainstorm/{id}`: fiecare `roundView` are `cards` (fara cele combinate) si `combined` ([]cardView, mereu non-nil); `cardView` are `mergedInto` (*int64) si `combinedFrom` ([]{"id","title"}, gol pe cele normale).

- [ ] **Step 1: Teste care pica**:
  - `TestCombineEndpoint`: 201 si cardul are sourceA/sourceB; a doua cerere cu o intrebare -> 409; agentId neparticipant -> 400; `{id}` (subiectul din URL) diferit de subiectul cardurilor -> 409.
  - `TestEditEndpoint`: 204 pe combinat done, 409 pe card normal.
  - `TestTopicCombinedShape`: dupa combinare, runda sursei noi are cardul in `combined`, nu in `cards`; sursele au `mergedInto` = id-ul combinatului; combinatul are `combinedFrom` cu ambele titluri.
- [ ] **Step 2:** `go test ./internal/web -run 'CombineEndpoint|EditEndpoint|TopicCombinedShape'` -> FAIL.
- [ ] **Step 3: Implementare**: ruta noua in server.go langa celelalte brainstorm; handler `combineCards` (topicID din URL merge in `Combine`, care verifica); "edit" in switch-ul din `cardAction`; in `getTopic` calculeaza `store.MergedSources(cards)` si titlurile pe id, si pune cardurile cu `Combined()` in `combined`.
- [ ] **Step 4:** testele trec, `go test ./internal/web`.
- [ ] **Step 5: Commit** `Brainstorm API: combine and edit`.

### Task 5: UI — tile-uri si modala ideii

**Files:**
- Modify: `internal/web/static/js/brainstorm.js` (`renderThread`, `card`, `cardBody`, `questionBody`, `renderPicked`), `internal/web/static/app.css` (blocul `.bs-*`, ~linia 1861-1946)
- Create (daca brainstorm.js trece de ~700 linii): `internal/web/static/js/brainstorm-idea.js` cu modala

**Interfaces:**
- Consumes: forma din Task 4 (`cards`, `combined`, `mergedInto`, `combinedFrom`, `edited`).
- Produces: `card(c)` intoarce tile-ul; `openIdea(c, siblings)` deschide modala (folosita si de Task 6).

- [ ] **Step 1: Tile-uri** (schita B): `.bs-cards` grid `repeat(4, minmax(0,1fr))` pe lat, 2 sub ~1100px, 1 pe telefon (media query existenta); toate tile-urile unui rand aceeasi inaltime (grid `align-items: stretch`, fara inaltime fixa daca textul e taiat). Tile: `border-top: 3px solid` culoarea agentului (`ui.toneOf` / culoarea avatarului), rand cu avatar + nume + rol, titlu (max 2 randuri, `-webkit-line-clamp`), text simplu (markdown curatat: fara `#`, `*`, ```` ``` ````) taiat la 3 randuri, jos etichetele Picked / Wild / Merged / Combined / Edited sau starea. Wild: gradient mov discret (fara chenar punctat). Intrebare deschisa: fundal teal, optiunile ca butoane mici pe tile + Other (deschide modala cu camp text, ca acum). Waiting / running / stopped / failed / skipped: tile estompat cu starea ("Waiting, busy on T-42", "Thinking…") si butonul lui (Run now / Stop / Retry) direct pe tile.
- [ ] **Step 2: Modala ideii**: click pe tile (nu pe butoane / bifa) -> modala cu autor, titlu, textul complet (md), "Based on", "Combined from: A + B" (link: inchide modala, deruleaza la tile, il evidentiaza 1.5 s), si actiunile Pick/Unpick, Another idea (nu pe combinate), Retry, Edit (doar combinat done: titlu + textarea, Save / Cancel -> `POST cards/{id}/edit`). ←/→ trec la tile-ul vecin din acelasi rand, Esc inchide. Se reimprospateaza la evenimentul SSE "brainstorm" daca cardul s-a schimbat.
- [ ] **Step 3: Picked**: zona de sus ramane, cu tile-uri mici (titlu + autor), click = modala.
- [ ] **Step 4: Verificare**: `node --check` pe copia .mjs; build + pornire locala pe alt port cu un HOME/date de test (NU reporni instanta utilizatorului daca `pgrep -f "claude -p"` gaseste ceva); un subiect cu 4 carduri reale-ish (unul wild, o intrebare deschisa, unul waiting, unul cu titlu de 120 si text lung) -> capturi Chrome headless prin CDP la 1440px si 390px; tile-urile au aceeasi inaltime pe rand si nimic nu iese din ele.
- [ ] **Step 5: Commit** `Brainstorm UI: idea tiles and idea modal`.

### Task 6: UI — selectie, bara Combine, randul Combined; docs

**Files:**
- Modify: `internal/web/static/js/brainstorm.js`, `internal/web/static/app.css`, `CLAUDE.md` (doar paragraful nou, vezi Step 4)

**Interfaces:**
- Consumes: Task 4 (endpoint-uri, forma), Task 5 (`card`, `openIdea`).

- [ ] **Step 1: Selectie**: bifa pe tile-urile idee/wild done (si combinate done), in coltul din dreapta sus; starea selectiei in memorie (Set de id-uri, pastrata la re-render, golita la schimbarea subiectului si dupa Combine). Bifa nu deschide modala.
- [ ] **Step 2: Bara de jos** (sticky la baza firului, deasupra compozitorului): apare cu >=1 bifat: "<N> selected · Combine with <select agent> · Clear · Combine". Select-ul: participantii subiectului, implicit autorul primei bife (daca nu mai e participant, primul participant). Combine activ doar cu exact 2, altfel dezactivat cu `title="Select two ideas"`. Combine -> `POST /api/brainstorm/{tid}/combine`; eroare -> toast cu mesajul serverului.
- [ ] **Step 3: Randul Combined**: dupa gridul rundei, daca `round.combined` nu e gol: eticheta "Combined" + grid de tile-uri; pe tile "Combined from: A + B" (link ca in modala). Sursele cu `mergedInto` au eticheta "Merged" (click: deruleaza la combinat). Captura CDP la 1440px cu un combinat waiting si unul done (editat).
- [ ] **Step 4: CLAUDE.md**: adauga dupa paragraful "Gata (Brainstorm, ...)" un paragraf "Gata (Brainstorm: combinare si tile-uri, spec in docs/superpowers/specs/2026-11-12-hobstack-brainstorm-combine-design.md): ..." (migrarea 039 source_a/source_b/edited, Combine / EditCombined, run fara sesiune si fara intrebari, MergedSources la citire, randul Combined, tile-uri + modala). ATENTIE: CLAUDE.md are modificari necomise ale altcuiva (paragraful despre README): comite DOAR hunk-ul tau (de exemplu `git diff CLAUDE.md` -> patch doar cu hunk-ul tau -> `git apply --cached`), lasa restul necomis. Nu atinge README.md si docs/images/.
- [ ] **Step 5:** `go vet ./...`, `go build -race ./...`, `go test ./...`, cross-compile darwin/windows, `node --check` pe JS.
- [ ] **Step 6: Commit** `Brainstorm UI: combine ideas`.
