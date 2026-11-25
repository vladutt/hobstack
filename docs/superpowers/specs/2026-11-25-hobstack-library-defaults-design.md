# Library: elemente default pentru toate proiectele

## De ce
Utilizatorul vrea ca un skill / server MCP / tool din Library sa poata fi pornit o data pentru toate proiectele, iar
efectul sa fie imediat, si in proiectele existente (nu doar un set copiat la crearea unui proiect). Un proiect care nu
vrea elementul il opreste doar pentru el.

## Obiective
- Flag "Default" pe un element publicat din Library (tool, skill, MCP). Cat e pornit, elementul intra in toate
  proiectele la urmatorul run, pe ultima versiune.
- Un proiect poate opri un element default (si il poate reporni).
- Cheile (secretele) unui MCP / tool default se completeaza o data, global; un proiect isi poate pune valoarea lui.

## Non-obiective
- Defaults per categorie de proiecte sau per agent.
- Seturi de pornire copiate la crearea proiectului.
- Agentii nu pot marca elemente ca default (doar utilizatorul, din UI).

## Anti-obiective
- Un element default nu trebuie sa ajunga intr-un proiect care l-a oprit, nici dupa un update de versiune, nici dupa
  ce flag-ul e scos si pus la loc.
- Secretele globale nu ies din API si nu apar in caile vizibile agentilor (ca secretele per proiect azi).

## Abordari
1. **Calculat la run (aleasa).** `library_items.is_default` + tabel de secrete globale; o functie
   `store.EffectiveInstalls(pid)` combina instalarile proiectului cu elementele default. Instalarea explicita castiga.
   O singura sursa de adevar: update-ul, proiectele noi si scoaterea flag-ului merg fara alte drumuri.
2. **Copiat in fiecare proiect.** Randuri in `library_installs` la marcare, la crearea proiectului, la fiecare update.
   Respinsa: nu se mai deosebeste "instalat de mine" de "venit din default", scoaterea flag-ului nu stie ce sa stearga,
   iar fiecare drum de creare a proiectului (formular, wizard) trebuie sa-si aminteasca sa copieze.

Utilizatorul a ales 1 si varianta A pentru chei (globale cu override per proiect, nu doar per proiect / interzis).

## Design

### Date (migrarea 041)
- `library_items.is_default INTEGER NOT NULL DEFAULT 0`.
- `library_default_secrets (item_id, name, value, PRIMARY KEY (item_id, name))`, item_id cu ON DELETE CASCADE.
- Optarea unui proiect: randul existent din `library_installs` cu `enabled = 0` (fara coloana noua). `version_id` e
  NOT NULL, deci randul de oprire primeste ultima versiune la momentul opririi (nu conteaza cat e oprit).

### Regula de rezolvare (`store.EffectiveInstalls(pid) []LibraryInstall`)
- Toate randurile din `library_installs` ale proiectului, ca azi (versiunea fixata, `enabled` al lor).
- Plus, pentru fiecare element cu `is_default = 1` si `status = published` care nu are rand in proiect: o instalare
  virtuala, `Enabled = true`, `VersionID` = cea mai noua versiune (aceeasi ordine ca `ListLibraryVersions`, `vs[0]`),
  marcata `FromDefault = true` (camp nou, `json:"fromDefault"`). Un element default fara nicio versiune e sarit.
- Elementele unpublished / declined nu se aplica ca default (scoaterea din Library le scoate peste tot).

Toate locurile care citesc azi `ListInstalls` pentru "ce are proiectul" trec pe `EffectiveInstalls`:
`library.Build` si `inputsHash`, `team.recordLibraryUses`, `team` mcp_requests (serverul "e deja instalat"),
`web.listProjectLibrary`, `web.putProjectLibrary` (vezi mai jos). `ListInstalls` ramane pentru ce e strict al proiectului.

### Pornirea flag-ului pe un element deja instalat (decizia utilizatorului: A)
- La pornirea flag-ului, randurile `enabled = 1` ale elementului din toate proiectele se sterg: acele proiecte trec pe
  default (ultima versiune, update-uri automate). Randurile `enabled = 0` raman si devin opriri.
- Cat elementul e default, Publish (`publishLocked`, care azi cheama `store.Install` pe proiectul sursa) nu mai scrie
  randul fixat pentru proiectul sursa si nu reporneste niciodata un rand `enabled = 0` (anti-obiectiv). Pentru elementele
  ne-default, Publish ramane cum e, dar tot fara sa reporneasca un rand oprit.
- Scoaterea flag-ului nu creeaza randuri: proiectele care il primeau doar din default il pierd (comportamentul ales la 1).

### Celelalte drumuri de pornire (decizia constructorului, aprobata)
- Toate drumurile care azi cheama `installInProject` (comutatorul din proiect, aprobarea / instalarea directa a unei
  cereri MCP, "Add "x" to this project" de pe cardul de skill refuzat, Add skill cu activare, Library > MCP > Add
  server) trec printr-un helper comun: pe un element default, sterg randul de oprire (urmeaza default-ul) si salveaza
  secretele per proiect daca au venit; pe un element ne-default, instaleaza ca azi.
- `install_mcp` de la un agent pe un element default oprit in proiect: randul de oprire NU conteaza ca "already
  installed"; cererea merge in Needs you ca pentru un element din Library neinstalat (aprobarea = helper-ul de mai sus).

### Secrete
- Cheile globale sunt un fallback general per element: se pot seta oricand (si fara flag), se folosesc de orice proiect
  care are elementul (default sau instalat explicit) fara valoare proprie, si raman la scoaterea flag-ului.
- `store.Secrets(pid, item)` ramane per proiect. Functie noua `store.EffectiveSecrets(pid, item)`: secretele globale
  ale elementului, suprascrise de valorile nevide ale proiectului. `library.Build` si `inputsHash` (HMAC-ul) folosesc
  `EffectiveSecrets`, deci schimbarea unei chei globale reface build-ul.
- Proba la Publish / install ramane cum e; la setarea cheilor globale nu se ruleaza proba.
- Un element default caruia ii lipseste o cheie si global si in proiect ramane oprit in proiectul acela, cu linia
  existenta "missing secret".

### Proiect: oprire / pornire (PUT /api/projects/{id}/library/{item})
- `installed: false` pe un element default: in loc de `Uninstall`, scrie randul cu `enabled = 0` (functie noua
  `store.DisableInstall(pid, item, versionID)`, upsert).
- `installed: true` pe un element default care are rand de oprire: sterge randul (revine la default, ultima versiune),
  apoi `skillInstalled` ca la o instalare noua. Daca utilizatorul alege explicit o versiune / upgrade, ramane rand
  explicit ca azi.
- Setarea secretelor per proiect pe un element default fara rand: permisa (nu mai cere "install the item first").
- Dupa orice schimbare: `hub.Publish(pid, "project", ...)` ca azi.

### Library: flag si chei globale
- `PUT /api/library/{id}/default` cu `{default?: bool, secrets?: {name: value}, clear?: [name]}` (fiecare camp
  optional; cheile se pot schimba fara flag). Pornirea flag-ului doar pe elemente published (409 altfel). Valorile nu se intorc niciodata; GET pe element intoarce `isDefault` si
  `defaultSecretsSet: [nume]`.
- La pornirea flag-ului: pentru fiecare proiect in care elementul devine activ (fara rand de oprire), acelasi efect ca
  o instalare: `team.SkillAvailable` (cardurile "wanted the skill" se inchid, agentul primeste nota) si
  `hub.Publish` pe proiect. La scoatere: doar `hub.Publish`.
- Unpublish pe un element default: flag-ul ramane in DB dar nu se aplica (regula de mai sus); Publish din nou il
  reactiveaza. (Apelul de calcul ignora oricum non-published.)

### UI
- Pagina item-ului din Library: comutator "Default for all projects" cu textul "On in every project unless a project
  turns it off". Pentru elementele cu chei: campurile cheilor globale ("Used by every project that has no own value").
  Sub comutator: "Off in N projects" (din randurile `enabled = 0`).
- Lista Library: eticheta "Default" pe elementele marcate.
- Instructions > Library pe proiect: elementele default apar cu eticheta "Default"; comutatorul existent le opreste /
  reporneste; cheile arata "Using the global value" cand proiectul nu are valoare proprie, cu posibilitatea de a o
  suprascrie.
- Textele in engleza, ca restul UI-ului.

### Teste
- store: EffectiveInstalls (default fara rand, rand explicit castiga, rand de oprire, unpublished ignorat, fara
  versiuni sarit), EffectiveSecrets (global, override, valoare goala nu suprascrie), migrarea.
- library: Build include un default, nu include un default oprit, hash-ul se schimba la cheie globala noua.
- web: PUT default (published only, secretele nu ies, pornirea sterge randurile enabled 1 si pastreaza opririle),
  PUT pe proiect (oprire = rand enabled 0, repornire = rand sters), Publish nu reporneste un rand oprit.
- team: install_mcp pe un element default oprit face cerere in Needs you.

## Lasat constructorului
- Forma exacta a UI-ului (asezarea comutatorului si a campurilor) dupa stilul existent al paginilor Library.
- Numele functiilor noi, daca exista deja ceva potrivit.
- Actualizarea CLAUDE.md (sectiunea Stare) la final.
