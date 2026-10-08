# Foreman: Library (tool-uri, skill-uri si servere MCP refolosite intre proiecte)

Data: 2026-10-08
Status: implementat (2026-10-08). Mockup-urile 09-13 din ../screenshots.

## Scop

Un agent face intr-un proiect ceva util si in alte proiecte (un script care genereaza PDF-uri, un set de reguli
pentru texte in romana, un server MCP read-only peste o baza de date). Azi ramane ingropat in repo-ul proiectului.
Library ii da un loc comun:

- **Tools**: scripturi pe care agentii le ruleaza din shell (`pdf-offer --data offer.json`).
- **Skills**: instructiuni si checklist-uri (`SKILL.md` + fisiere) pe care Claude le incarca singur cand task-ul
  se potriveste (`when: task edits user-facing text`).
- **MCP**: servere care dau agentilor acces live la un sistem (local, pornit de claude; sau remote, prin URL).

Agentii **propun**, utilizatorul **publica**. Nimic nu ajunge in alt proiect fara un click al utilizatorului.

In afara scopului (pentru acum): magazin / sincronizare intre masini, versiuni cu ramuri, rularea tool-urilor
intr-un sandbox separat (ruleaza ca orice comanda Bash a agentului, cu aceleasi permisiuni), pagina "Agents"
din bara de sus (e sub-proiectul "librarie de agenti" din CLAUDE.md, separat).

## Cum ajung la agenti

Mecanismul e `claude --plugin-dir <dir>` (o sesiune, fara instalare globala), verificat in `claude --help` 2.1.x:
un plugin poate avea `skills/`, `.mcp.json` si fisiere oarecare.

La fiecare run, Foreman construieste (sau refoloseste, dupa un hash) un plugin per proiect:

```
~/.foreman/library/build/<project-id>/
  .claude-plugin/plugin.json      {"name": "foreman-library"}
  skills/<skill>/SKILL.md (+ fisiere)
  .mcp.json                       serverele MCP activate in proiect, cu secretele proiectului
  bin/<tool>                      scripturile publicate (copiate, executabile)
```

si adauga `--plugin-dir` + o linie in prompt: `Library tools are in <dir>/bin: pdf-offer, vat-calc. Run them with Bash.`

**Rezultatele spike-ului** (claude 2.1.x, in modul nostru `--setting-sources user --strict-mcp-config`):
1. skill-urile din `--plugin-dir` se incarca si cu `--setting-sources user`: merge asa cum e planificat.
2. `.mcp.json` din plugin este ignorat sub `--strict-mcp-config`. Serverele MCP ajung deci prin `--mcp-config`-ul
   nostru (cel cu serverul `team`); cand un server are secrete, config-ul se scrie intr-un fisier 0600 (sters la
   sfarsitul run-ului) si `--mcp-config` primeste calea, ca secretele sa nu apara in linia de comanda.
3. Tool-urile MCP (`mcp__<server>__*`) trebuie permise explicit in `--settings`; Foreman adauga serverul in allow.

Decizii schimbate fata de propunerea initiala:
- Directorul pluginului e per proiect si per hash al intrarilor (`build/<proiect>-<hash>/`), nu doar per proiect;
  se refoloseste cat timp intrarile nu se schimba.
- Probe-ul serverului MCP local (`tools/list`) ruleaza la Publish, nu la propunere: porneste un proces in afara
  sistemului de permisiuni al lui claude, deci nu il lasam sa depinda de ce scrie un agent. O propunere MCP locala
  cere tool-ul Shell la agent, iar pana la Publish apare ca "Not tested".
- "Keep in project" nu publica niciodata; elementele create de utilizator se publica direct.
- "Recent use": un tool conteaza cand o comanda Bash contine `/bin/<nume>/`, un server MCP cand run-ul apeleaza
  `mcp__<nume>__*`; un rand per element si run, doar pentru run-uri pe task (chat-ul nu se noteaza). Skill-urile
  nu se urmaresc.

## Date

```
library_items
  id, kind ('tool'|'skill'|'mcp'), name (unic per kind, [a-z0-9-]), description,
  status ('proposed'|'published'|'declined'|'unpublished'),
  source_project_id, source_task_id, made_by_agent_id (NULL daca l-a facut utilizatorul),
  needs TEXT  -- JSON: ["network", "write", "browser", "php"]
  secrets TEXT -- JSON: ["META_TOKEN"] (doar numele; valorile sunt per proiect)
  mcp_transport ('local'|'remote'), mcp_url (remote), created_at, published_at
library_versions
  id, item_id, version ('1.3'), files_path (~/.foreman/library/items/<id>/<version>/), lines, created_at, note
library_installs
  project_id, item_id, version_id, enabled, PRIMARY KEY (project_id, item_id)
library_secrets
  project_id, item_id, name, value  -- in baza locala, ca si restul datelor; nu se trimit nicaieri
library_uses
  item_id, task_id, agent_id, at  -- "Recent use" din pagina unui tool (din comenzile Bash / tool_use MCP)
```

Fisierele unei versiuni sunt copiate din repo-ul proiectului in `~/.foreman/library/items/...` la publicare: o
schimbare ulterioara in repo nu schimba ce ruleaza celelalte proiecte.

## Cum propune un agent (tool MCP nou)

`propose_library_item(kind, name, description, files, needs, secrets, when?)`, pentru orice agent cu tool-ul Write:

- `files`: caile din folderul agentului (max 20 fisiere, max 1 MB total); pentru `skill`, `SKILL.md` e obligatoriu;
  pentru `mcp` local, o comanda de pornire (`command`, `args`) si lista de tool-uri expuse (din `tools/list`, vezi mai jos).
- Aplicatia copiaza fisierele intr-o zona de propuneri si creeaza `library_items` cu `status = proposed`.
- Raspuns: `Proposed meta-post to the library. The user decides in Needs you; keep using it in this project meanwhile.`
- In chat apare cardul din mockup 13 ("Share to Tool library?"), iar in "Needs you" o cerere (ca `agent_requests`).
- Limite: 3 propuneri deschise per proiect; acelasi nume deschis o data.

Pentru `mcp` local, inainte de propunere aplicatia porneste serverul o data (timeout 10 s), cere `tools/list` si
salveaza lista + "N/N tools respond" (randul verde din mockup 11). Un server care nu raspunde nu poate fi propus.

## Ce face utilizatorul

- **Publish**: `status = published`, versiunea 1.0 (sau +0.1 la o versiune noua a aceluiasi nume). Proiectul sursa il
  are instalat automat; celelalte il vad in Library si il instaleaza cu un click (din pagina item-ului sau din
  Instructions > Library). Un item cu `secrets` cere valorile la instalare ("Each project provides").
- **Review code**: fisierele propunerii, cu diff fata de versiunea publicata (refolosim `changes.js`).
- **Decline** / **Keep in <project>**: ramane instalat doar in proiectul sursa (`status = declined`, install pastrat).
- **Unpublish**: dispare din Library; proiectele care il au instalat il pastreaza la versiunea curenta, marcat.

## UI (mockup-uri)

- Bara de sus: `Projects | Library` (Agents vine cu sub-proiectul lui).
- `#/library/tools`, `#/library/skills`, `#/library/mcp`: titlurile mari Tools / Skills / MCP, filtre All / Mine /
  Recently added (la MCP: All / By agents / External), cautare, "+ New tool / skill / server".
- Sus, "Proposed · needs you": cardurile propunerilor (Review code / Decline / Publish).
- Tools: tabel (Tool, What it does, Made by, Version). Skills: grila de carduri + panou de previzualizare in dreapta.
  MCP: tabel (Server, Description, Runs, Tools, Made by) + cardul propunerii cu "Exposes" si "Each project provides".
- Pagina unui item (`#/library/tools/<name>`, mockup 12): Usage / Code / Versions, "Recent use", Type, Made by,
  Approved by, Version, Needs, Unpublish.
- Agentul are in editorul lui o lista "Library" (ce item-uri instalate in proiect poate folosi; implicit toate).

## Siguranta

- Un tool publicat ruleaza ca orice comanda Bash a agentului: are nevoie de tool-ul Shell si de deny-ul fix
  (rm -rf, sudo, git push raman blocate). "Needs: network" e doar informativ pana avem sandbox.
- Secretele: doar in `library_secrets`, puse in `.mcp.json` / mediul run-ului proiectului care le-a dat; nu intra in
  prompt, in log-ul de UI sau in propunerea copiata in alte proiecte.
- Codul unei propuneri nu ruleaza in alt proiect pana nu e publicat; publicarea arata mereu fisierele (Review code).

## Etape de implementare

1. Spike: verificarile de mai sus cu `--plugin-dir` (o zi). Daca skills/MCP din plugin nu merg in modul nostru,
   alternativa: skill-urile copiate in `--add-dir` + text in prompt, MCP prin `--mcp-config` al nostru.
2. Store + migrare, fara UI: items, versiuni, instalari, build-ul pluginului per proiect.
3. Skills (cele mai simple: doar fisiere text).
4. Tools (scripturi in `bin/`, "Recent use" din comenzile Bash).
5. MCP local si remote, secrete per proiect.
6. Propunerile agentilor (`propose_library_item`) si cardul din chat.
