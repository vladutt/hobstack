# Foreman: cont Claude per proiect

Data: 2026-10-09
Status: spec (neimplementat).

## Scop

Utilizatorul vrea sa separe munca de personal: proiectele firmei ruleaza pe abonamentul Claude Team al firmei,
restul pe contul personal (cel logat in CLI). Azi toate run-urile `claude` folosesc login-ul din `~/.claude`.

Decizii luate cu utilizatorul:
- Contul de munca e un **abonament Claude Team**: autentificare prin `CLAUDE_CODE_OAUTH_TOKEN` (generat cu
  `claude setup-token`), nu `CLAUDE_CONFIG_DIR` si nu cheie API.
- Un proiect sta pe **un singur cont**, ales de utilizator; fara comutare automata.
- **Fail closed**: un proiect cu cont nu cade niciodata pe contul implicit.
- Schimbarea contului pe proiect **reseteaza sesiunile** agentilor (ca "New chat"), ca o conversatie sa nu treaca
  dintr-un cont in altul prin `--resume`.

In afara scopului: chei API / Bedrock / Vertex / `ANTHROPIC_BASE_URL`, comutare automata la rate limit, cont per agent,
cost separat per cont in Stats, mai multe directoare de config.

## De ce `CLAUDE_CODE_OAUTH_TOKEN`

Verificat cu claude 2.1.295:
- variabila are prioritate fata de login-ul din keychain (`claude auth status` arata `authMethod: "oauth_token"`);
- `configDirectory` si `projectsDirectory` raman `~/.claude`: setarile user (`--setting-sources user`), pluginurile si
  sesiunile raman aceleasi, deci `--resume` merge in continuare;
- `auth status` **nu** valideaza token-ul si nu arata contul; validarea cere un run real (vezi Check).

`CLAUDE_CONFIG_DIR` ar fi schimbat si setarile user si locul sesiunilor; respins.

## Date

Migrarea 026 (025 e rezervata de spec-ul pentru mutarea/stergerea task-urilor):

```
claude_accounts
  id               INTEGER PRIMARY KEY
  name             TEXT NOT NULL UNIQUE      -- ex. "Work"
  token            TEXT NOT NULL             -- CLAUDE_CODE_OAUTH_TOKEN
  created_at       DATETIME NOT NULL
  checked_at       DATETIME                  -- ultima verificare reusita
  last_error       TEXT NOT NULL DEFAULT ''  -- ultima eroare (Check sau run cu eroare de autentificare)

projects.account_id  INTEGER REFERENCES claude_accounts(id)   -- NULL = Default (CLI-ul logat)
```

- Token-ul nu iese niciodata din API: raspunsurile au `hasToken` si `tokenHint` (ultimele 4 caractere).
- `DeleteAccount` refuza daca un proiect il foloseste (`ErrAccountInUse`, "Used by N projects").
- Store: `Accounts()`, `Account(id)`, `AddAccount`, `UpdateAccountToken`, `RenameAccount`, `DeleteAccount`,
  `SetAccountCheck(id, err string)` (gol = reusit: seteaza `checked_at`, goleste `last_error`), `ProjectAccountEnv(pid)`.

## Rulare

- `runner.Spec` primeste `Env []string` (perechi `K=V` adaugate peste mediu).
- `Manager.Start`: `cmd.Env = cleanEnv(os.Environ())` + `spec.Env`. `cleanEnv` scoate **mereu**
  `CLAUDE_CODE_OAUTH_TOKEN` si `ANTHROPIC_API_KEY` mostenite din shell-ul care a pornit foreman, ca un proiect pe
  Default sa nu ia alt cont din mediu.
- `store.ProjectAccountEnv(pid)` intoarce `["CLAUDE_CODE_OAUTH_TOKEN=..."]` sau nil pentru Default.
- Cine il foloseste (contul proiectului in care ruleaza agentul):
  - run-urile echipei (`internal/team/run.go`), inclusiv conversatiile din chat;
  - draft-ul de agent (`internal/draft`);
  - wizard-ul pe folder si chat-ul din wizard (`internal/setup`, proiect existent);
  - proiectele legate: munca si intrebarile ajung la radacina proiectului **tinta**, care ruleaza pe contul tintei.
- Wizard-ul "Proiect nou cu AI": sesiunea retine `accountID` ales la pasul 1; analiza si chat-ul ruleaza cu el, iar la
  creare devine `projects.account_id`.
- Probele din Library nu ruleaza `claude`; neschimbate.

## Erori de autentificare (fail closed)

- Daca `account_id` e setat dar contul nu mai exista sau nu are token: run-ul nu porneste; `startFailed` (pauza +
  nota "Cannot start <agent>: Claude account <name> has no token. The project is paused.").
- Un run care se termina cu eroare de autentificare: `runner.IsAuthError(outcome)` cauta in `Error`/`Result`
  (case-insensitive) `401`, `authentication_error`, `invalid api key`, `oauth token`, `/login`. Atunci:
  `SetAccountCheck(id, mesaj)`, proiectul trece pe pauza (anti-bucla, ca la `startFailed`), task-ul revine in Queued.
  Pe Default doar pauza + nota (nu avem rand de cont).
- "Needs you": cardul **Claude account failed** apare pentru proiectul al carui cont are `last_error` nevid:
  "Claude account ‹Work› failed: ‹mesaj›" cu **Open Settings**. Dispare cand un Check reuseste; atunci utilizatorul
  porneste echipa (Start team), fara reluare automata.

## Schimbarea contului pe proiect

- `PUT /api/projects/{id}` cu `accountId` diferit:
  - refuzat cu 409 "Stop the team first." daca proiectul are run-uri active;
  - altfel, intr-o tranzactie: `account_id` nou + pentru fiecare agent al proiectului ce face "New chat"
    (`session_id = ''`, `prompt_hash = ''`, `chat_from` = ultimul mesaj). Istoricul ramane, vizibil prin "Earlier messages".
- Sesiunile wizard-ului nu sunt afectate (sunt doar in memorie si legate de crearea proiectului).

## Check

`POST /api/accounts/{id}/check` (si automat dupa Add / Replace token), timeout 60 s:

```
claude -p "Reply with OK" --model haiku --output-format json \
  --setting-sources user --tools ""
```

ruland intr-un director temporar gol (fara tool-uri, deci o singura tura), cu `cleanEnv` + token-ul contului. Un `result` fara `is_error` e reusit;
altfel mesajul merge in `last_error`. Costa cat un mesaj scurt; nu intra in Total cost.
`GET /api/accounts/default` ruleaza `claude auth status` si intoarce `email` + `subscriptionType` (doar informativ).

## API

- `GET /api/accounts` → lista (fara token) + `projects` (cate proiecte il folosesc) + `default` (email, abonament).
- `POST /api/accounts` `{name, token}`; `PUT /api/accounts/{id}` `{name?, token?}`; `DELETE /api/accounts/{id}`.
- `POST /api/accounts/{id}/check`.
- Proiect: `accountId` (0/absent = Default) in GET/POST/PUT; `accountName` in lista de proiecte.
- Setup: `accountId` in cererea de pornire a analizei.

## UI

**Settings > Claude accounts** (sectiune noua in `settings.js`, deasupra regulilor pentru comenzi):
- Rand fix "Default · the logged-in CLI" cu email si abonament.
- Per cont: nume, `••••abcd`, stare ("Works · checked 2h ago" / eroarea in rosu), "Used by N projects",
  **Check**, **Replace token**, **Delete** (dezactivat cand e folosit).
- **+ Add account**: nume + token; hint: "Log in with the work account in a terminal and run `claude setup-token`;
  paste the token here." Contul se salveaza si daca Check-ul pica (apare rosu).

**Formularul de proiect** ("How the team works"): select **Claude account** (Default + conturile). La schimbare pe un
proiect existent, confirmare inline: "Agents start new Claude sessions on the new account. Earlier messages stay saved."

**Wizard "Proiect nou cu AI"**: acelasi select la pasul 1, langa "Model for the analysis".

**Vizibilitate**: badge cu numele contului in tabelul Proiecte si in header-ul proiectului, doar pentru proiectele
care nu sunt pe Default.

## Teste

- store: migrarea, CRUD, `DeleteAccount` refuzat cand e folosit, schimbarea contului goleste sesiunile agentilor.
- runner: `cleanEnv` + `Spec.Env` (cu cont, fara cont, variabile mostenite scoase); `IsAuthError` pe mesaje reale.
- team: proiect cu cont fara token → `startFailed`; outcome cu eroare de autentificare → pauza + `last_error`.
- API: token-ul nu apare in niciun raspuns; 409 la schimbarea contului cu run-uri active.
- JS: `node --check` pe fisierele modificate (ca .mjs); screenshot Chrome headless pentru Settings si formularul de proiect.
- `go vet ./...`, `go build -race`, cross-compile darwin/arm64 si windows.

## Ordine

1. Migrarea 026 + store (conturi, `account_id`, schimbarea contului cu reset de sesiuni).
2. runner: `Spec.Env`, `cleanEnv`, `IsAuthError`; folosit in team, draft, setup.
3. Erori de autentificare: pauza, `last_error`, cardul din Needs you.
4. API conturi + Check + default.
5. UI: Settings, formularul de proiect, wizard, badge.
