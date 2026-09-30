# Chat cu Foreman in wizard: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** La pasul 3 al wizard-ului (ambele moduri), un panou de chat "Foreman" in care utilizatorul cere schimbari; Foreman (analistul, reluat cu `--resume`) actualizeaza direct propunerea, cu Undo.

**Architecture:** `internal/setup` retine sesiunea Claude a analistului, conversatia, istoricul propunerilor si starea "busy"; `Chat` porneste un run nou al analistului cu `--resume` si propunerea curenta in tura; `propose_setup` e acceptat si in timpul chat-ului; `finish` deosebeste run-ul de chat. Web: rutele chat/undo. UI: panou in dreapta pe pagina de review, cu activitate live si reconstruirea formularului.

**Tech Stack:** Go, vanilla JS (embed), Claude Code CLI headless.

**Spec:** `docs/superpowers/specs/2026-09-30-foreman-wizard-chat-design.md`

## Global Constraints

- `go vet ./...`, `go build -race ./...`, `go test ./...` trec dupa fiecare task; cross-compile `GOOS=darwin GOARCH=arm64 go build ./cmd/foreman` si `GOOS=windows go build ./cmd/foreman`.
- JS: fiecare fisier din `internal/web/static/js/` se verifica ca modul: `cp f /tmp/x.mjs && node --check /tmp/x.mjs` (`node --check` pe `.js` nu prinde erorile de sintaxa).
- JS: o valoare optionala folosita apoi cu `?.` e `cond ? x : null`, nu `cond && x`; `replaceChildren`/`append` native NU aplatizeaza array-uri (foloseste spread sau `el()`).
- app.css: o clasa care seteaza `display` bate atributul `hidden`; adauga regula `[hidden]` pentru containerele ascunse.
- Tot ce vede utilizatorul si prompturile sunt in engleza; comentariile in romana, fara diacritice.
- Analistul ramane doar-citire: aceleasi argumente de siguranta (`--permission-mode dontAsk`, `--setting-sources user`, deny pe scriere, MCP doar `propose_setup`).
- Limite: mesaj de chat maxim 4000 de caractere; istoric Undo maxim 20 de versiuni.
- Lock-ul `m.mu` din `setup.Manager`: niciun apel la `m.starter` cu lock-ul tinut; campurile sesiunii se citesc/scriu sub lock.

## Review Focus

1. **Sesiunea fara folder:** `--resume` trebuie rulat in acelasi cwd ca analiza (Claude tine sesiunile pe cwd). Folderul temporar nu se mai sterge la finalul analizei, ci la Dismiss / curatenia de 24h. Test in Task 1.
2. **Foreman adauga singur shell/web** in chat: refuzat; pastreaza shell deja bifat de utilizator: acceptat. Test in Task 2.
3. **Run de chat fara schimbare** (doar o intrebare): Undo nu trebuie sa sara peste ultima schimbare reala (versiunea pusa in istoric la inceput se scoate). Test in Task 2.
4. **Finalul unui run de chat nu schimba statusul sesiunii** (ramane `ready`, chiar daca run-ul esueaza); finalul analizei ramane ca azi. Test in Task 2.
5. **Formularul nu pierde editarile:** campurile se blocheaza cat lucreaza Foreman; la final se reface doar daca `version` a crescut. Verificat in Task 4 (browser).

---

### Task 1: Refactor: argumentele analistului si folderul temporar

**Files:**
- Modify: `internal/setup/manager.go` (Session.addDirs/cwd, args(s, model, turn), finish, Dismiss, cleanupOld, Start, StartFolder)
- Test: `internal/setup/manager_test.go`

**Interfaces:**
- Produces:
  - `func (m *Manager) args(s *Session, model, turn string) []string`: `-p turn`; `--add-dir` pentru fiecare cale din `s.addDirs`
  - `Session.cwd string`, `Session.addDirs []string` (private): calculate la Start / StartFolder
  - folderul temporar al unei sesiuni fara folder traieste pana la Dismiss sau curatenia de 24h

- [ ] **Step 1: Testele**
  - In `TestManagerNoProposalFails`, asertia „folderul temporar trebuia sters” devine: folderul temporar **exista inca** dupa finish.
  - Dupa `m.Dismiss(s.ID)`, folderul nu mai exista.
  - Adauga un test ca `Start` cu 2 foldere si `StartFolder` produc aceleasi `--add-dir` ca azi. Testele existente `TestManagerSeveralFolders` si `TestStartFolder` deja verifica asta; ruleaza-le.

- [ ] **Step 2: RED** — `go test ./internal/setup/`

- [ ] **Step 3: Implementarea**
  - In `Session` adauga campurile private `cwd string` si `addDirs []string`.
  - **`Start`:** `s.cwd` = `dirs[0]` sau folderul temporar; `s.addDirs` = `dirs[1:]`. Spec-ul run-ului foloseste `Dir: s.cwd`, `Args: m.args(s, model, turnPrompt(s))`.
  - **`StartFolder`:** `s.cwd` = folderul analizat; `s.addDirs` = celelalte foldere ale proiectului (bucla de azi). Spec-ul foloseste `m.args(s, model, turnPrompt(s))` fara `--add-dir` adaugate separat.
  - **`args(s, model, turn)`:** `-p` primeste `turn`, iar la final, pentru fiecare `dir` din `s.addDirs`, adauga `--add-dir dir`. Scoate bucla veche pe `s.Workdirs[min(1,…):]`.
  - **`finish`:** nu mai sterge `s.tempDir`.
  - **`Dismiss`:** dupa ce ascunde sesiunea (si, daca ruleaza, o opreste), sterge `s.tempDir` daca nu e gol si sesiunea nu ruleaza. Pentru o sesiune care ruleaza, folderul se sterge la curatenie.
  - **`cleanupOld`:** cand sterge o sesiune, sterge si `s.tempDir`.

- [ ] **Step 4: GREEN** — `go test ./internal/setup/ ./internal/web/ && go vet ./...`

- [ ] **Step 5: Commit** — `git commit -m "setup: argumentele analistului cu tura data; folderul temporar traieste cat sesiunea"`

---

### Task 2: Setup: Chat, propose in chat, finalul chat-ului, Undo

**Files:**
- Modify: `internal/setup/manager.go`, `internal/setup/prompt.go`
- Test: `internal/setup/chat_test.go` (nou)

**Interfaces:**
- Consumes: `args(s, model, turn)`, `s.cwd` (Task 1)
- Produces:
  - `type ChatMessage struct { Role, Text string; At time.Time }` (JSON `role, text, at`; Role: `"user"`, `"foreman"` sau `"event"`)
  - `Session.Chat []ChatMessage` (JSON `chat`, mereu non-nil), `Session.Busy bool` (`busy`), `Session.ChatRunID string` (`chatRunId`), `Session.Version int` (`version`)
  - `var ErrBusy = errors.New("Foreman is still working; wait for the answer")`
  - `var ErrNotReady = errors.New("the proposal is not ready yet")`
  - `func (m *Manager) Chat(id, message string, current Proposal) (Session, error)`
  - `func (m *Manager) Undo(id string) (Session, error)`

- [ ] **Step 1: Testele** — `internal/setup/chat_test.go`:

```go
package setup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foreman/internal/runner"
)

// readySession: o analiza terminata cu o propunere (Ana + Bob) si sesiunea Claude "sid-1".
func readySession(t *testing.T) (*Manager, *fakeStarter, Session) {
	t.Helper()
	m, f := newTest(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0o644)
	s, err := m.Start([]string{dir}, "a shop", "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	b := m.Wrap(innerBackend{})
	c, _ := b.Resolve(tokenOf(t, f.specs[0]))
	prop := `{"name":"Shop","goal":"g","context":"c","agents":[{"name":"Ana","role":"Lead","reportsTo":""},{"name":"Bob","role":"Dev","reportsTo":"Ana","tools":["write"]}]}`
	if _, err := b.ProposeSetup(c, json.RawMessage(prop)); err != nil {
		t.Fatal(err)
	}
	f.done[s.RunID](runner.Outcome{RunID: s.RunID, Status: "done", SessionID: "sid-1"})
	got, _ := m.Get(s.ID)
	if got.Status != StatusReady {
		t.Fatalf("%+v", got)
	}
	return m, f, got
}

func TestChatResumesWithCurrentProposal(t *testing.T) {
	m, f, s := readySession(t)
	cur := *s.Proposal
	cur.Agents[1].Tools = []string{"write", "shell"} // utilizatorul a bifat shell la Bob
	cur.Context = "edited by the user"
	got, err := m.Chat(s.ID, "add a QA agent", cur)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Busy || got.ChatRunID == "" || len(got.Chat) != 1 || got.Chat[0].Role != "user" {
		t.Fatalf("%+v", got)
	}
	spec := f.specs[len(f.specs)-1]
	if argAfter(spec.Args, "--resume") != "sid-1" || spec.Dir != s.Workdirs[0] {
		t.Fatalf("resume=%q dir=%q", argAfter(spec.Args, "--resume"), spec.Dir)
	}
	if !strings.Contains(spec.Prompt, "edited by the user") || !strings.Contains(spec.Prompt, "User: add a QA agent") {
		t.Fatalf("tura:\n%s", spec.Prompt)
	}
	if _, err := m.Chat(s.ID, "again", cur); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy: %v", err)
	}

	// Foreman propune: pastreaza shell la Bob (ok); adauga shell la Vera (refuzat)
	b := m.Wrap(innerBackend{})
	c, _ := b.Resolve(tokenOf(t, spec))
	bad := `{"name":"Shop","goal":"g","context":"edited by the user","agents":[{"name":"Ana","role":"Lead","reportsTo":""},{"name":"Bob","role":"Dev","reportsTo":"Ana","tools":["write","shell"]},{"name":"Vera","role":"QA","reportsTo":"Ana","tools":["shell"]}]}`
	if _, err := b.ProposeSetup(c, json.RawMessage(bad)); err == nil || !strings.Contains(err.Error(), "shell") {
		t.Fatalf("shell nou: %v", err)
	}
	good := strings.Replace(bad, `"tools":["shell"]}`, `"tools":["write"]}`, 1)
	if _, err := b.ProposeSetup(c, json.RawMessage(good)); err != nil {
		t.Fatal(err)
	}
	f.done[spec.ID](runner.Outcome{RunID: spec.ID, Status: "done", SessionID: "sid-2", Result: "Added Vera (QA)."})
	got, _ = m.Get(s.ID)
	if got.Busy || got.Status != StatusReady || len(got.Proposal.Agents) != 3 || got.Version != s.Version+1 {
		t.Fatalf("dupa chat: %+v", got)
	}
	if last := got.Chat[len(got.Chat)-1]; last.Role != "foreman" || last.Text != "Added Vera (QA)." {
		t.Fatalf("%+v", last)
	}
	// Undo readuce propunerea trimisa de utilizator (cu editarile lui)
	u, err := m.Undo(s.ID)
	if err != nil || len(u.Proposal.Agents) != 2 || u.Proposal.Context != "edited by the user" {
		t.Fatalf("undo: %v %+v", err, u.Proposal)
	}
	// al doilea chat reia sesiunea noua
	m.Chat(s.ID, "rename Bob", *u.Proposal)
	if argAfter(f.specs[len(f.specs)-1].Args, "--resume") != "sid-2" {
		t.Fatal("trebuia reluata sesiunea sid-2")
	}
}

// un run de chat fara schimbare (doar o intrebare) nu lasa o intrare falsa pentru Undo
func TestChatQuestionOnlyKeepsUndo(t *testing.T) {
	m, f, s := readySession(t)
	m.Chat(s.ID, "why Bob?", *s.Proposal)
	spec := f.specs[len(f.specs)-1]
	f.done[spec.ID](runner.Outcome{RunID: spec.ID, Status: "done", Result: "Because."})
	if _, err := m.Undo(s.ID); err == nil {
		t.Fatal("nu exista nicio schimbare de anulat")
	}
	// un run de chat esuat: statusul ramane ready, eroarea apare in chat
	m.Chat(s.ID, "x", *s.Proposal)
	spec = f.specs[len(f.specs)-1]
	f.done[spec.ID](runner.Outcome{RunID: spec.ID, Status: "failed", Error: "boom"})
	got, _ := m.Get(s.ID)
	if got.Status != StatusReady || got.Busy || !strings.Contains(got.Chat[len(got.Chat)-1].Text, "boom") {
		t.Fatalf("%+v", got)
	}
}

func TestChatValidation(t *testing.T) {
	m, _, s := readySession(t)
	if _, err := m.Chat(s.ID, "  ", *s.Proposal); err == nil {
		t.Fatal("mesaj gol")
	}
	if _, err := m.Chat(s.ID, strings.Repeat("x", 4001), *s.Proposal); err == nil {
		t.Fatal("mesaj prea lung")
	}
	bad := *s.Proposal
	bad.Agents = nil
	if _, err := m.Chat(s.ID, "hi", bad); err == nil {
		t.Fatal("propunere invalida")
	}
	if _, err := m.Chat("nu-exista", "hi", *s.Proposal); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
	// o analiza care ruleaza inca nu are chat
	m2, _ := newTest(t)
	running, _ := m2.Start(nil, "notes app", "sonnet")
	if _, err := m2.Chat(running.ID, "hi", *s.Proposal); !errors.Is(err, ErrNotReady) {
		t.Fatalf("%v", err)
	}
}
```

Adauga si un test pentru modul folder: `StartFolder`, propunere, finish cu `SessionID`, apoi `Chat`. Run-ul are `--resume`, cwd-ul e folderul analizat si are `--add-dir` pentru celelalte foldere. O propunere in chat cu un nume de agent existent e refuzata.

- [ ] **Step 2: RED** — `go test ./internal/setup/`

- [ ] **Step 3: Campuri si erori**

In `Session`:

```go
	Chat      []ChatMessage `json:"chat"`      // conversatia cu Foreman dupa analiza; mereu non-nil
	Busy      bool          `json:"busy"`      // un run de chat e in curs
	ChatRunID string        `json:"chatRunId"` // run-ul de chat in curs
	Version   int           `json:"version"`   // creste la fiecare propunere noua sau Undo

	claudeSession string              // sesiunea Claude a analistului, pentru --resume
	history       []*Proposal         // versiunile anterioare (Undo), maxim 20
	chatTools     map[string][]string // tool-urile permise per agent (litere mici) in chat-ul curent
	chatChanged   bool                // run-ul de chat curent a propus ceva
```

`ChatMessage` si erorile, ca in Interfaces. `Chat` se initializeaza `[]ChatMessage{}` in Start / StartFolder. `Version` creste in `propose` la fiecare propunere acceptata (si in analiza).

- [ ] **Step 4: `Chat`**

```go
// Chat trimite un mesaj lui Foreman (analistul, reluat cu --resume) despre
// propunere. current e propunerea din pagina, cu editarile utilizatorului.
func (m *Manager) Chat(id, message string, current Proposal) (Session, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return Session{}, ValidationError("the message is empty")
	}
	if err := tooLong("message", message, 4000); err != nil {
		return Session{}, err
	}
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return Session{}, ErrNotFound
	}
	if s.Busy {
		m.mu.Unlock()
		return Session{}, ErrBusy
	}
	if s.Status != StatusReady || s.claudeSession == "" {
		m.mu.Unlock()
		return Session{}, ErrNotReady
	}
	existing := s.existing
	m.mu.Unlock()

	if err := ValidateProposal(&current, false, existing); err != nil {
		return Session{}, err
	}
	tools := map[string][]string{}
	for _, a := range current.Agents {
		tools[strings.ToLower(a.Name)] = a.Tools
	}
	turn, err := chatTurn(current, message)
	if err != nil {
		return Session{}, err
	}

	m.mu.Lock()
	if s.Busy { // alt mesaj a castigat intre timp
		m.mu.Unlock()
		return Session{}, ErrBusy
	}
	// versiunea trimisa (cu editarile utilizatorului) devine propunerea curenta si
	// intra in istoric: Undo dupa schimbarea lui Foreman o readuce exact asa
	p := current
	s.Proposal = &p
	s.history = append(s.history, &p)
	if len(s.history) > 20 {
		s.history = s.history[len(s.history)-20:]
	}
	s.Chat = append(s.Chat, ChatMessage{Role: "user", Text: message, At: time.Now()})
	s.Busy, s.ChatRunID, s.chatTools, s.chatChanged = true, runner.NewID(), tools, false
	delete(m.tokens, s.token)
	s.token = randomHex(16)
	m.tokens[s.token] = s.ID
	args := append(m.args(s, s.Model, turn), "--resume", s.claudeSession)
	spec := runner.Spec{ID: s.ChatRunID, Key: "setup-" + s.ID, Dir: s.cwd, Args: args, Prompt: turn}
	m.mu.Unlock()

	if _, err := m.starter.Start(spec, func(o runner.Outcome) { m.finish(s.ID, o) }); err != nil {
		m.finish(s.ID, runner.Outcome{RunID: spec.ID, Status: "failed", Error: err.Error()})
		return Session{}, err
	}
	return m.snapshot(s), nil
}
```

(Daca `m.args` citeste `s.token`, apelul sub lock e corect: e doar construire de sir, fara apel la starter.)

`chatTurn` in `prompt.go`:

```go
// chatTurn: mesajul pentru un run de chat (propunerea curenta + cererea utilizatorului).
func chatTurn(current Proposal, message string) (string, error) {
	raw, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return "", err
	}
	return "The user is reviewing your proposal and asks for a change.\n" +
		"Current proposal (with the user's own edits; build on it, do not revert them):\n" + string(raw) + "\n\n" +
		"User: " + message + "\n\n" +
		"If this changes the proposal, call propose_setup once with the WHOLE updated proposal, then reply in one or two sentences with what you changed. " +
		"If it is only a question, answer it without calling propose_setup. " +
		"Do not add the shell or web tools yourself; keep the tools the user already chose.", nil
}
```

(Importa `encoding/json` in `prompt.go`.)

- [ ] **Step 5: `propose` in chat**

In `propose`, sub primul lock citeste si `busy := s.Busy` si `chatTools := s.chatTools`. Conditia de acceptare devine `running || busy`. In chat, validarea e:

```go
	if busy {
		if err := ValidateProposal(&p, false, existing); err != nil {
			return "", err
		}
		for _, a := range p.Agents {
			for _, tool := range a.Tools {
				if tool == "write" || tool == "git" || slices.Contains(chatTools[strings.ToLower(a.Name)], tool) {
					continue
				}
				return "", ValidationError(fmt.Sprintf("you may not add the shell or web tools (%s: %s); the user adds them", a.Name, tool))
			}
		}
	} else if err := ValidateProposal(&p, true, existing); err != nil {
		return "", err
	}
```

In modul folder, completarea numelui gol se face inaintea validarii, ca azi. Apoi `CheckFolderPaths` / `CheckFolderProposal`, ca azi. La scriere, sub lock, re-verifica `s.Status == StatusRunning || s.Busy`, apoi `s.Proposal = &p; s.Version++; if s.Busy { s.chatChanged = true }`. Titlul se schimba doar in afara modului folder si doar in analiza (`!s.Busy`), ca azi.

- [ ] **Step 6: `finish`**

La inceputul lui `finish`, dupa ce gasesti sesiunea:

```go
	if s.Busy && o.RunID == s.ChatRunID {
		m.finishChat(s, o)
		return
	}
	if s.Status != StatusRunning {
		return
	}
	// ... restul ca acum (fara stergerea folderului temporar), plus:
	if o.SessionID != "" {
		s.claudeSession = o.SessionID
	}
```

```go
// finishChat inchide un run de chat; statusul sesiunii ramane ready. Se cheama cu m.mu tinut.
func (m *Manager) finishChat(s *Session, o runner.Outcome) {
	delete(m.tokens, s.token)
	s.Busy, s.ChatRunID = false, ""
	if o.SessionID != "" {
		s.claudeSession = o.SessionID
	}
	// un run care n-a schimbat propunerea nu lasa o intrare pentru Undo
	if !s.chatChanged && len(s.history) > 0 {
		s.history = s.history[:len(s.history)-1]
	}
	now := time.Now()
	switch {
	case o.Status == "stopped":
		s.Chat = append(s.Chat, ChatMessage{Role: "event", Text: "Stopped.", At: now})
	case o.Status == "failed":
		s.Chat = append(s.Chat, ChatMessage{Role: "event", Text: "Foreman failed: " + firstNonEmpty(o.Error, o.Result, "unknown error"), At: now})
	default:
		s.Chat = append(s.Chat, ChatMessage{Role: "foreman", Text: firstNonEmpty(o.Result, "Done."), At: now})
	}
}
```

Atentie: istoricul contine versiunea pusa la inceputul chat-ului, iar propunerea de la inceputul chat-ului devine `s.Proposal`. Daca run-ul nu a schimbat nimic, `s.Proposal` ramane propunerea trimisa, deci editarile utilizatorului se pastreaza. Scoti doar intrarea din istoric.

- [ ] **Step 7: `Undo` si `Stop`**

```go
// Undo readuce propunerea dinaintea ultimei schimbari.
func (m *Manager) Undo(id string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	if s.Busy {
		return Session{}, ErrBusy
	}
	if len(s.history) == 0 {
		return Session{}, ValidationError("there is nothing to undo")
	}
	s.Proposal = s.history[len(s.history)-1]
	s.history = s.history[:len(s.history)-1]
	s.Version++
	s.Chat = append(s.Chat, ChatMessage{Role: "event", Text: "Undid the last change.", At: time.Now()})
	return *s, nil
}
```

`Stop(id)`: cand `s.Busy`, opreste `s.ChatRunID`; altfel `s.RunID`, ca azi.

- [ ] **Step 8: GREEN** — `go test -race ./internal/setup/ && go vet ./... && go test ./...` + cross-compile.

- [ ] **Step 9: Commit** — `git commit -m "setup: chat cu Foreman dupa analiza (resume, propunere curenta, Undo)"`

---

### Task 3: Web: rutele chat si undo

**Files:**
- Modify: `internal/web/api_setup.go`, `internal/web/server.go`
- Test: `internal/web/setup_test.go`

**Interfaces:**
- Produces:
  - `POST /api/setup/{id}/chat {message, proposal}` → 202 cu sesiunea. 400 la validare, 404, 409 cand e busy sau nu e gata.
  - `POST /api/setup/{id}/undo` → 200 cu sesiunea. 404, 409 (busy), 400 cand nu e nimic de anulat.

- [ ] **Step 1: Test** in `setup_test.go`, folosind managerul real cu `nopStarter` si o sesiune adusa in `ready` prin MCP si `finish`:
  - `nopStarter` nu cheama `onDone`, deci construieste sesiunea ready prin `wizard.New` cu un starter fals local testului web, care retine `onDone` (ca `fakeStarter` din setup). Daca e prea complicat, testeaza doar codurile: 404 pe o sesiune inexistenta si 409 (`ErrNotReady`) pe o sesiune care inca ruleaza.
  - Codurile verificate: 202 pe o sesiune ready, 409 pe una busy, 400 pe mesaj gol.
  - Maparea erorilor merge in `setupFail`: `ErrBusy` si `ErrNotReady` dau 409.

- [ ] **Step 2: RED**, **Step 3:** handlere:

```go
func (s *Server) chatSetup(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeErr(w, http.StatusNotFound, wizard.ErrNotFound.Error())
		return
	}
	var in struct {
		Message  string          `json:"message"`
		Proposal wizard.Proposal `json:"proposal"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	sess, err := s.wizard.Chat(r.PathValue("id"), in.Message, in.Proposal)
	if err != nil {
		setupFail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, sess)
}

func (s *Server) undoSetup(w http.ResponseWriter, r *http.Request) {
	if s.wizard == nil {
		writeErr(w, http.StatusNotFound, wizard.ErrNotFound.Error())
		return
	}
	sess, err := s.wizard.Undo(r.PathValue("id"))
	if err != nil {
		setupFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}
```

In `setupFail`, inainte de `fail(w, err)`:

```go
	if errors.Is(err, wizard.ErrBusy) || errors.Is(err, wizard.ErrNotReady) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
```

Rutele: `POST /api/setup/{id}/chat` si `POST /api/setup/{id}/undo`.

- [ ] **Step 4: GREEN**, **Step 5: Commit** — `git commit -m "api: chat si undo in wizard"`

---

### Task 4: UI: panoul Foreman la pasul 3

**Files:**
- Create: `internal/web/static/js/foreman-chat.js`
- Modify: `internal/web/static/js/setup.js`, `internal/web/static/app.css`

**Interfaces:**
- Consumes: sesiunea (`chat`, `busy`, `chatRunId`, `version`, `proposal`), rutele din Task 3, `/api/info` (`name`), `liveRenderer` din `live.js`.
- Produces: `foremanChat({ session, appName, folderMode, collect, onChanged })` → `{ el, setDisabled(bool), destroy() }`:
  - `collect()` intoarce propunerea curenta din formular (acelasi obiect ca la Create / Add agents);
  - `onChanged(session)` e chemat cand `version` a crescut sau dupa Undo.

- [ ] **Step 1: Colectarea propunerii** (`setup.js`):
  - Extrage din `create()` si din `createInFolder()` construirea obiectului propunere intr-o functie `collectProposal()`. In modul proiect nou, folosesti numele, obiectivul, contextul, `folders` cu `folderName` si agentii bifati. In modul folder, folosesti numele proiectului, folderul cu contextul din textarea si agentii.
  - `create()` si `createInFolder()` o folosesc. Comportamentul nu se schimba.

- [ ] **Step 2: Asezarea**
  - `stepReview(p)` pune continutul de azi intr-o coloana `.wizard-main` si adauga in dreapta `.wizard-side`, cu panoul de chat.
  - Containerul e `el("div", { class: "wizard-review-cols" }, main, side)`.
  - Butoanele finale (Back / Create project / Add agents) raman in coloana principala.

- [ ] **Step 3: Componenta** `foreman-chat.js`:
  - Header cu numele aplicatiei, capitalizat.
  - Mesajul de start:
    - proiect nou: "I proposed this team. Tell me what to change: add or remove agents, rename them, rewrite instructions or context.";
    - modul folder: "…I only add new agents here; your existing team stays as it is."
  - Lista de mesaje:
    - `user` aliniat dreapta;
    - `foreman` aliniat stanga;
    - `event` centrat si discret.
  - Campul de scris: Enter trimite, iar Shift+Enter face rand nou.
  - **La trimitere:**
    1. `setDisabled(true)` si blocarea formularului: `onBusy(true)` cheama `setup.js`, care pune `inert` pe `.wizard-main` sau dezactiveaza input-urile.
    2. `POST /api/setup/{id}/chat` cu `{ message, proposal: collect() }`.
    3. Adauga mesajul tau si caseta live `"<Name> is working…"`, cu `liveRenderer` pe `EventSource(/api/runs/{chatRunId}/events)`.
    4. La `app_done` sau la eroarea stream-ului, `GET /api/setup/{id}`, de cateva ori cat timp `busy` e true, ca `checkResult`.
    5. Redeseneaza conversatia din `session.chat`. Daca `session.version` > versiunea de dinainte, cheama `onChanged(session)` si pune un buton **Undo** pe ultimul mesaj `foreman`.
    6. Deblocheaza.
  - **Undo:** `POST /api/setup/{id}/undo`, apoi `onChanged(session)`, apoi redeseneaza conversatia.
  - **La redeschidere,** daca `session.busy`, se reia stream-ul pe `chatRunId`.

- [ ] **Step 4: Reconstruirea formularului** (`setup.js`, `onChanged`):
  - Tine minte propunerea veche (agenti dupa nume, cu campurile lor serializate).
  - Cheama `stepReview(session.proposal)`, pastrand aceeasi instanta de chat: panoul se muta in noua coloana laterala, nu se recreeaza, ca sa nu se piarda conversatia si stream-ul.
  - Adauga clasa `.changed` pe cardurile agentilor noi sau schimbati si scoate-o dupa 4 secunde.
  - Deruleaza la primul card schimbat.

- [ ] **Step 5: CSS**

```css
/* chat cu Foreman in wizard */
.wizard-review-cols { display: grid; grid-template-columns: minmax(0, 1fr) clamp(300px, 30vw, 400px); gap: 18px; align-items: start; }
.wizard-side { position: sticky; top: 16px; }
.foreman-chat { background: var(--card); border: 1px solid var(--line); border-radius: var(--radius); display: flex; flex-direction: column; height: calc(100vh - 44px); min-height: 360px; }
.foreman-chat .fc-head { padding: 12px 14px; border-bottom: 1px solid var(--line); font-weight: 600; }
.foreman-chat .fc-thread { flex: 1; overflow: auto; padding: 12px 14px; display: grid; gap: 10px; align-content: start; }
.fc-msg { max-width: 90%; padding: 8px 10px; border-radius: 10px; white-space: pre-wrap; overflow-wrap: anywhere; font-size: 13px; }
.fc-msg.user { justify-self: end; background: var(--amber-bg); border: 1px solid var(--line-2); }
.fc-msg.foreman { justify-self: start; background: var(--card-2); }
.fc-msg.event { justify-self: center; color: var(--muted); font-size: 12px; }
.foreman-chat form { display: flex; gap: 8px; padding: 10px 14px; border-top: 1px solid var(--line); }
.foreman-chat textarea { flex: 1; min-width: 0; }
.wizard-agent.changed, .folder-row.changed { outline: 2px solid var(--amber); outline-offset: 2px; transition: outline-color .6s; }
@media (max-width: 1000px) { .wizard-review-cols { grid-template-columns: minmax(0, 1fr); } .wizard-side { position: static; } .foreman-chat { height: 480px; } }
```

- [ ] **Step 6: Verificare** — bucla `.mjs`, `go test ./internal/web/`, `go vet ./...`.

- [ ] **Step 7: Commit** — `git commit -m "ui: chat cu Foreman la pasul 3 al wizard-ului"`

---

### Task 5: Verificare in browser si CLAUDE.md

- [ ] **Step 1:** Verificarea completa: vet, race, test, cross-compile, `.mjs`.
- [ ] **Step 2:** `claude`-ul fals. Cand tura contine „User: add a QA agent”:
  1. citeste propunerea curenta din tura (JSON-ul de dupa „Current proposal”);
  2. adauga agentul „Vera” (QA, `reportsTo`: primul agent);
  3. apeleaza `propose_setup`;
  4. raspunde „Added Vera (QA).”.
  
  Cand tura contine „User: why”, raspunde doar cu text. La final scrie un `session_id` in evenimentul `init`, ca analiza sa aiba o sesiune de reluat.
- [ ] **Step 3:** Parcurge fluxul prin CDP:
  1. Analiza pe un folder, pasul 3. Panoul „Foreman” e in dreapta.
  2. Editeaza manual contextul.
  3. Scrie „add a QA agent”. Verifica: Vera apare evidentiata, contextul editat e pastrat, raspunsul e in chat, butonul Undo apare.
  4. Apasa Undo. Vera dispare.
  5. Scrie „why Bob?”. Formularul ramane neschimbat.
  6. Refa fluxul in modul folder.
  7. Testeaza si la 1000px.
  
  Fa screenshot-uri si citeste-le.
- [ ] **Step 4:** In CLAUDE.md, la „Stare”, adauga o linie:

  ```
  Gata (chat in wizard, spec in docs/superpowers/specs/2026-09-30-foreman-wizard-chat-design.md): la pasul 3, panoul "Foreman" reia sesiunea analistului (--resume) si schimba propunerea la cerere, cu Undo.
  ```

  Apoi commit.
