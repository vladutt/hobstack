# Foreman: search si Install pentru servere MCP in Library

Data: 2026-10-09
Status: implementat (2026-10-09).

## Scop

Azi un server MCP intra in Library doar propus de un agent (`propose_library_item`) sau facut de mana (comanda de pornire
scrisa de utilizator). Utilizatorul vrea: "cand vreau sa adaug un MCP, sa pot da doar search dupa unul si Install".

Decizii luate cu utilizatorul:
- Sursa principala: **registrul oficial MCP** (registry.modelcontextprotocol.io) — search, apoi Install.
- Sursa secundara: **serverele din configurarile Claude ale utilizatorului** (`~/.claude.json` si Claude Desktop),
  in acelasi ecran, sub "From your Claude".

In afara scopului: lipirea unui JSON `mcpServers`, actualizarea automata la versiuni noi din registru, servere remote
care cer header-e (Authorization etc.), servere locale care cer argumente de pozitie obligatorii fara valoare implicita.

## 1. Ecranul

Library -> MCP -> buton "Add MCP server" (pagina `#/library/mcp/add`):

- Camp de cautare sus (debounce 300 ms, minim 2 caractere; fara text: primele rezultate ale registrului). Rezultate:
  titlu (sau nume), descriere (o linie), tipul (`local · npx` / `local · uvx` / `local · docker` / `remote`), versiunea,
  si butonul **Install**. Doar ultima versiune a fiecarui server. "Load more" cand registrul are `nextCursor`.
- Daca registrul nu raspunde (timeout, rata depasita): mesaj "The MCP registry is not reachable right now. Try again in a
  minute." + buton Retry; sectiunea "From your Claude" ramane utilizabila.
- Sectiunea **From your Claude**: serverele din `~/.claude.json` (cheia `mcpServers` de sus si cele din `projects.*`)
  si din Claude Desktop (`~/Library/Application Support/Claude/claude_desktop_config.json` pe macOS; `%APPDATA%\Claude\`
  pe Windows), cu sursa ("Claude Code" / "Claude Desktop") si Install. Fisierele lipsa sunt ignorate.
- Un server care exista deja in Library (acelasi nume, sau aceeasi comanda+argumente / acelasi URL) apare "In library".

## 2. Install

Fereastra de install arata:
- **Name in Library**: sugerat din numele serverului (`io.github.user/foo-mcp` -> `foo-mcp`), forma `[a-z0-9-]`, unic;
  editabil.
- **Ce va rula**: pentru local, comanda exacta construita (vezi mai jos); pentru remote, URL-ul.
- **Variabile de mediu**: lista numelor (cu descrierea din registru si "required" / "secret"). Toate devin `Secrets`
  ale elementului (nume; valorile se dau per proiect in Instructions > Library, ca azi). Din configurarile Claude se iau
  doar numele variabilelor, NICIODATA valorile.
- Bifa "Enable in <proiectul curent>" daca utilizatorul a venit dintr-un proiect (link "Add MCP server" din
  Instructions > Library), altfel lipsa.
- Butonul **Install**: confirma comanda exacta (ca la Publish azi), apoi:
  1. creeaza elementul `kind = mcp`, `status = published`, `made_by` = You, cu `transport`, `command` + `args` sau `url`,
     `secrets`, `description` (din registru / "From Claude Code config");
  2. testeaza: pentru local `library.ProbeMCP(command, args, env, 30s)`, cu env = valorile secretelor deja date in
     proiectul curent (daca exista), altfel gol; pentru remote, aceeasi proba pe care o face azi Publish pentru remote
     (sau fara proba, daca azi nu exista);
  3. proba reusita -> `tool_names` salvate; proba picata -> elementul NU se creeaza, iar fereastra arata eroarea si
     "Install without test" (dupa o confirmare), care il creeaza cu `tool_names` gol — ca "Publish without test" azi.
  4. "Enable in proiect" bifat -> instalat si activat in proiect (acelasi drum ca butonul de instalare de azi).

Construirea comenzii din registru (doar `packages` cu `transport.type = "stdio"`):
- `registryType = npm`  -> `npx -y <identifier>@<version>`
- `registryType = pypi` -> `uvx <identifier>==<version>`
- `registryType = oci`  -> `docker run -i --rm <identifier>:<version>` (+ `-e NAME` pentru fiecare variabila de mediu)
- `packageArguments` cu `isRequired` si fara valoare implicita -> serverul e afisat dar Install e dezactivat cu motivul
  "Needs arguments Foreman can't fill in yet". Argumentele cu valoare implicita se adauga.
- Remote: primul `remotes[]` cu `type` `streamable-http` sau `sse`; daca are `headers` obligatorii -> Install dezactivat
  ("Needs headers; not supported yet").
- Alt `registryType` / transport -> nu se arata.

## 3. Siguranta

- Instalarea ruleaza cod pe masina (npx / uvx / docker): comanda exacta e afisata si confirmata inainte de proba.
- Cererile catre registru le face serverul foreman (GET, fara credentiale), cu timeout 10 s si cache in memorie 10 min
  pe (search, cursor); browserul nu iese din localhost.
- Valorile variabilelor din configurarile Claude nu se citesc in UI si nu se salveaza (doar numele).
- Credentialele din comanda, argumentele si URL-urile din configurarile Claude (parola / token in URL, parametri si
  optiuni cu nume de credential, NAME=valoare, header-e, JSON cu chei de credential, si in argumentele cu spatii) devin
  `<hidden>` inainte sa iasa din server, iar serverul nu se poate instala ("Has a credential in its arguments or URL; move it
  to env in your Claude config, then install"). Limita acceptata: un secret aflat doar in calea unui URL sau o valoare fara
  nume dupa care sa fie recunoscut (de ex. calea unui webhook Slack) nu poate fi gasit; utilizatorul confirma comanda
  exacta inainte de Install.
- Numele, descrierea si comanda din registru sunt date externe: afisate prin `el()`, comanda trece prin validarea
  existenta (`library.ValidateInput`).

## 4. Server

- `GET /api/library/registry?search=&cursor=` -> `{ servers: [ { id, name, title, description, version, kind:
  "local"|"remote", runtime: "npx"|"uvx"|"docker"|"", command, args[], url, env: [{name, description, required,
  secret}], suggestedName, installable, reason, inLibrary } ], nextCursor }` (normalizat din `/v0/servers?search=&cursor=
  &version=latest`, doar ce se poate construi; pachet nou `internal/mcpregistry`).
- `GET /api/library/local-mcp` -> `{ servers: [ { source: "Claude Code"|"Claude Desktop", name, kind, command, args,
  url, env: [{name}], suggestedName, inLibrary } ] }` (fara valori).
- `POST /api/library/install-mcp` `{ name, description, kind, command, args, url, secrets[], skipTest, projectId? }` ->
  201 element | 409 nume luat | 422 `{ error, testFailed: true }` cand proba pica (fara skipTest).

## 5. Teste

- mcpregistry: normalizarea pe raspunsuri reale salvate ca fisiere de test (npm, pypi, oci, remote, remote cu headers,
  argument obligatoriu), cache, timeout -> eroare clara (server de test httptest).
- local configs: citirea ambelor fisiere (top-level + projects), fisier lipsa / JSON invalid ignorat, valorile env nu ies.
- install: creare publicata, nume luat 409, proba picata 422 si nimic creat, skipTest creeaza cu tool_names gol, enable in
  proiect; proba cu un server MCP de test (exista deja in testele library).
- web/UI: screenshot-uri (instanta goala, claude fals, registrul inlocuit de un server de test local prin variabila de
  mediu `FOREMAN_MCP_REGISTRY_URL` doar pentru teste) la 1440 si 390: cautare, rezultate, fereastra de install, eroare de
  registru.

## Etape

1. `internal/mcpregistry` (client + normalizare + cache) si citirea configurarilor Claude.
2. Install (store/library + web) cu proba.
3. UI (pagina Add MCP server, fereastra de install, link din Instructions > Library).
4. CLAUDE.md.
