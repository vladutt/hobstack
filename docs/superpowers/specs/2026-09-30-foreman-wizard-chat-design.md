# Foreman: chat cu Foreman in wizard, dupa analiza

Data: 2026-09-30
Status: design aprobat in chat.

## Scop

La pasul 3 al wizard-ului (propunerea), utilizatorul vorbeste cu AI-ul, numit ca aplicatia ("Foreman"), ca sa
schimbe propunerea: agenti noi sau scosi, redenumiri, instructiuni, context. Merge in ambele wizard-uri: "New project
with AI" si "Set up agents with AI" (modul folder).

## Decizii (din brainstorming)

- **Aplicare:** Foreman actualizeaza direct toata propunerea; formularul se reface, partile schimbate sunt evidentiate
  cateva secunde; un buton **Undo** readuce versiunea anterioara.
- **Editarile manuale** din formular se trimit la fiecare mesaj; Foreman construieste pe ele.
- **Ambele wizard-uri.** In modul folder raman regulile lui (doar agenti noi, sefi existenti permisi).
- **Asezare:** panou "Foreman" in dreapta (sticky), propunerea in stanga; pe ecran ingust, sub propunere.
- **Tool-uri:** Foreman poate pastra tool-urile deja bifate de utilizator in propunerea curenta, dar nu poate adauga
  singur `shell` sau `web` (ca la analiza).
- **Numele** "Foreman" vine din `config.AppName` (prin `/api/info`), capitalizat.

## Backend (`internal/setup`, `internal/web`)

### Sesiunea

`Session` primeste:
- `claudeSession string` (privat): id-ul sesiunii Claude a analistului (`runner.Outcome.SessionID` la finalul analizei,
  actualizat dupa fiecare run de chat);
- `Chat []ChatMessage` (JSON `chat`): `{role: "user"|"foreman"|"event", text, at}`;
- `Busy bool` (JSON `busy`) si `ChatRunID string` (JSON `chatRunId`): un run de chat in curs;
- `Version int` (JSON `version`): creste la fiecare propunere noua sau Undo;
- `history []*Proposal` (privat, maxim 20): versiunile anterioare, pentru Undo;
- `chatTools map[string][]string` (privat): tool-urile permise per agent in run-ul de chat curent.

### `Chat(id, message string, current Proposal) (Session, error)`

- Sesiunea trebuie sa fie `ready` (propunere existenta), cu `claudeSession` nevid, si sa nu fie `Busy`
  (altfel `ErrBusy` -> 409 "Foreman is still working").
- `message`: obligatoriu, maxim 4000 de caractere.
- `current` se valideaza ca la creare (`ValidateProposal(&current, false, existing)`; in modul folder cu numele
  existente) si devine `s.Proposal`; versiunea anterioara intra in `history`.
- `chatTools` = pentru fiecare agent din `current`, tool-urile lui (nume cu litere mici -> lista).
- Porneste un run al analistului: aceleasi argumente ca la analiza (doar citire, `--permission-mode dontAsk`,
  `--setting-sources user`, deny pe scriere, MCP doar cu `propose_setup`, acelasi system prompt, in modul folder si
  paragraful lui; aceleasi `--add-dir`), plus `--resume <claudeSession>`, un token MCP nou, cwd-ul analizei (primul
  folder, folderul analizat, sau ACELASI folder temporar al analizei: Claude tine sesiunile pe cwd, deci `--resume`
  dintr-un alt folder nu gaseste sesiunea). Folderul temporar nu se mai sterge la finalul analizei, ci la Dismiss si la
  curatenia de 24h.
- Mesajul turei (`chatTurn`):
  ```
  The user is reviewing your proposal and asks for a change.
  Current proposal (with the user's own edits; build on it, do not revert them):
  <JSON-ul propunerii curente>
  User: <mesaj>
  If this changes the proposal, call propose_setup once with the WHOLE updated proposal, then reply in one or two
  sentences with what you changed. If it is only a question, answer it without calling propose_setup.
  Do not add the shell or web tools yourself; keep the tools the user already chose.
  ```
- `Chat` adauga `{role:"user", text}` in `Chat`, seteaza `Busy`, `ChatRunID` si intoarce sesiunea.

### `propose_setup` in timpul chat-ului

- Accepta propunerea cand sesiunea e `running` (analiza) SAU `Busy` (chat).
- In chat: `ValidateProposal(&p, false, existing)` (fara strictTools), apoi pentru fiecare agent: orice tool in afara
  de `write`/`git` trebuie sa fie in `chatTools` pentru acel agent (altfel eroare catre analist:
  "you may not add the shell or web tools (X: shell); the user adds them"). Apoi `CheckFolderPaths` /
  `CheckFolderProposal` ca la analiza (in modul folder, numele gol se completeaza ca acum).
- Propunerea noua inlocuieste `s.Proposal`, `Version++`, iar `changed = true` pentru run-ul curent.

### Finalul unui run de chat

`finish` deosebeste run-ul de chat (`o.RunID == s.ChatRunID`) de analiza:
- `Busy=false`, `ChatRunID=""`, token-ul MCP sters;
- `claudeSession` = `o.SessionID` (daca nevid);
- in `Chat`: `{role:"foreman", text: o.Result}` (sau, la esec, `{role:"event", text:"Foreman failed: <eroare>"}`;
  la stop, `{role:"event", text:"Stopped."}`);
- daca run-ul NU a schimbat propunerea, versiunea pusa in `history` la inceput se scoate (Undo ramane pe
  schimbarea anterioara reala);
- statusul sesiunii ramane `ready`.

### Undo

`Undo(id) (Session, error)`: cand nu e `Busy` si `history` e nevid: `s.Proposal = pop(history)`, `Version++`,
`Chat += {role:"event", text:"Undid the last change."}`. Altfel 409.

### Rute

- `POST /api/setup/{id}/chat {message, proposal}` -> 202 sesiunea (400 validare, 404, 409 busy/nu e gata).
- `POST /api/setup/{id}/undo` -> 200 sesiunea (404, 409).
- `POST /api/setup/{id}/stop` opreste si run-ul de chat, daca e `Busy`.
- `GET /api/setup/{id}` include `chat`, `busy`, `chatRunId`, `version`.

## UI (`setup.js`, pasul 3)

- Pagina de review devine doua coloane: propunerea (stanga) si panoul **"<AppName>"** (dreapta, sticky, inaltimea
  ecranului); sub ~1000px panoul trece sub propunere.
- Panoul: mesaj de start (in modul folder mentioneaza ca echipa existenta nu se schimba), conversatia (user dreapta,
  Foreman stanga, evenimente centrate/discrete), campul de scris (Enter trimite, Shift+Enter rand nou) si **Send**.
- La trimitere: se colecteaza propunerea curenta din formular (aceeasi colectare ca la Create / Add agents),
  `POST chat`, apoi activitatea live din `/api/runs/{chatRunId}/events` (liveRenderer) sub "Foreman is working…";
  campul e blocat.
- La final (`app_done`): `GET /api/setup/{id}`; daca `version` a crescut, formularul se reface din noua propunere,
  agentii noi sau schimbati (dupa nume si campuri) primesc un contur amber cateva secunde si pagina se duce la primul;
  in chat apare raspunsul si, daca s-a schimbat propunerea, un buton **Undo** pe acel mesaj (doar pe ultimul).
- **Undo:** `POST undo`, formularul se reface.
- Starea chat-ului (conversatia) vine din sesiune, deci la redeschiderea `#/setup/<id>` apare din nou; un run de
  chat in curs se reia live.
- Numele din `/api/info` (`name`), capitalizat.

## Cazuri limita

- Serverul repornit: sesiunea nu mai exista (eroarea de azi la incarcare).
- Run de chat esuat: mesaj de eroare in chat; propunerea neschimbata.
- Foreman propune o propunere invalida: primeste eroarea prin MCP si corecteaza (ca la analiza).
- Un mesaj trimis cat Foreman lucreaza: butonul e blocat; API-ul intoarce 409.
- Utilizatorul editeaza formularul cat Foreman lucreaza: la final, daca Foreman a schimbat propunerea, formularul se
  reface din versiunea lui (care porneste de la ce s-a trimis); editarile facute intre timp se pierd — campurile sunt
  blocate cat lucreaza Foreman, ca sa nu se intample.

## Teste

- setup: Chat porneste run cu `--resume <sid>`, cu propunerea curenta in tura; 409 cand e busy / fara sesiune;
  propose in chat actualizeaza versiunea; tool nou shell refuzat, shell pastrat acceptat; run fara schimbare nu lasa
  Undo gol; Undo; finish de chat nu schimba statusul; modul folder.
- web: rutele chat/undo (coduri, JSON).
- Browser (CDP + claude fals): mesaj "add a QA agent" -> agent nou evidentiat, raspuns in chat, Undo il scoate;
  editare manuala trimisa si pastrata.

## In afara scopului

- Chat dupa crearea proiectului (cu echipa existenta).
- Pastrarea conversatiei dupa restart.
- Streaming token cu token al raspunsului (se afiseaza activitatea live si raspunsul final).
