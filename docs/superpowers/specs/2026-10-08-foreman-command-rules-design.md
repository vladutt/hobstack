# Foreman: reguli pentru comenzi (Settings + override pe proiect + drept de apel)

Data: 2026-10-08
Status: aprobat in conversatie, de implementat.

## Scop

Azi lista de comenzi blocate e fixa in cod (`denyAlways` din `internal/prompt/settings.go`: `git push`, `rm -rf`,
`sudo`, `brew install`) si nu exista nicio cale sa o schimbi sau sa aprobi o exceptie. Utilizatorul vrea:

- o pagina **Settings** la nivelul aplicatiei, unde decide ce comenzi sunt voie si ce nu;
- **override pe proiect** (aici `git push` e voie, aici `docker` e interzis);
- **drept de apel**: cand un agent e refuzat pe o comanda „de intrebat”, utilizatorul o poate aproba din Needs you.

In afara scopului: un nivel per agent (au ramas tool-urile bifate), aprobari live care opresc run-ul
(`--permission-prompt-tool`), sandbox real. Regulile raman o plasa de siguranta, nu o izolare.

## Model

O **regula** = un prefix de comanda (`git push`, `rm -rf`, `docker`) + o stare:

| Stare | Ce se intampla cand un agent cu Shell incearca comanda |
|---|---|
| **Allow** | Merge. |
| **Ask** | Refuzata pe loc (run-ul continua); apare in Needs you, unde utilizatorul decide. |
| **Block** | Refuzata, fara intrebare. |

- **Niveluri:** globale (Settings) si per proiect. O regula a proiectului inlocuieste regula globala cu acelasi
  prefix si poate adauga prefixe noi. Fara nicio regula care se potriveste, comanda e permisa (ca azi, pentru
  agentii cu Shell).
- **Potrivire:** comanda se taie pe `&&`, `||`, `;`, `|` (ca in `bashMatches`); pentru fiecare bucata, castiga
  regula cu **cel mai lung prefix** care se potriveste (cuvinte intregi: `git push` prinde `git push origin`, nu
  `git pushx`). Daca vreo bucata e Block -> Block; altfel daca vreo bucata e Ask -> Ask; altfel permisa.
- **Pentru claude:** Ask si Block intra amandoua in `permissions.deny` din `--settings` ca `Bash(<prefix>:*)`.
  Allow nu adauga nimic (Shell permite deja tot `Bash`); un Allow doar scoate prefixul din deny.
- **Limita claude:** `deny` castiga mereu fata de `allow`. Un Allow mai lung sub un Ask/Block mai scurt
  (`git` = Block, `git status` = Allow) nu poate fi exprimat. UI-ul avertizeaza la salvarea unei astfel de combinatii;
  in `--settings` prefixul scurt ramane in deny (cel strict castiga).
- **Tool-urile agentului raman neschimbate:** Write, Shell, Install packages, Git, Web. Regulile se aplica doar
  comenzilor din terminal ale agentilor cu Shell. Un agent fara Shell ramane cu ce ii dau tool-urile (Git = lista de
  comenzi git, Install packages = instalarile).
- **Instalarile de pachete** raman legate de tool-ul Install packages (nu devin reguli).
- **Raman fixe si nu apar in Settings:** `TaskCreate`, `TaskUpdate`, `TaskList`, `TaskGet`, `TodoWrite`, `Agent`
  (coordonarea trece prin Foreman) si scrierea doar in folderul agentului.
- **Regulile implicite** (puse de migrare, si de „Reset to defaults”): `git push` = Ask, `rm -rf` = Ask,
  `brew install` = Ask, `sudo` = Block. Comportamentul fata de azi: aceleasi comenzi sunt refuzate, dar trei dintre
  ele pot fi aprobate.

Prefix valid: 1..80 caractere dupa normalizarea spatiilor, doar cuvinte separate prin spatii, fara caracterele
`& | ; $ \` ( ) < > *` si fara ghilimele. Stocat normalizat (spatii simple, fara spatii la capete), case-sensitive.

## Dreptul de apel (Ask)

1. Un agent cu Shell incearca o comanda pe Ask; claude o refuza (`permission_denials`, citite deja de
   `recordDenials` in `internal/team/run.go`).
2. `recordDenials` calculeaza regula care a prins comanda (functia de potrivire de mai sus, pe regulile efective ale
   proiectului). Daca e **Ask**: cerere in Needs you cu `prefix` completat (refolosim `access_requests`; incercari
   repetate ale aceluiasi agent pe acelasi prefix se aduna, cu contor, ca azi). Daca e **Block**: nicio cerere (doar
   evenimentul din istoricul task-ului, ca azi pentru lista fixa). Refuzurile tool-urilor fixe raman ignorate.
3. Cardul din Needs you: „**Rook** wants to run `git push origin main`” (ultima comanda exacta), task-ul, contorul,
   si butoanele:
   - **Allow once:** un grant pentru (proiect, agent, prefix). La urmatorul run al acestui agent, prefixul e scos din
     deny; grant-ul se marcheaza folosit cand run-ul porneste (un singur run).
   - **Allow in this project:** regula proiectului `prefix = Allow` (pentru toti agentii cu Shell din proiect).
   - **Block:** regula proiectului `prefix = Block`.
   - **Dismiss:** inchide cererea; data viitoare se intreaba din nou.
4. Dupa **Allow once** / **Allow in this project**: agentul primeste un mesaj nelivrat (il trezeste, ca la
   raspunsul la `ask_user`): `You may now run "<comanda>" (<allowed once | allowed in this project> by the user).
   Run it again if you still need it.` Daca cererea are task si task-ul nu e Done, task-ul revine in coada (ca la
   `ask_user`). Cererea se inchide.
5. **Full autonomy** nu schimba nimic aici: cererile Ask raman la utilizator (radacina nu poate largi permisiuni).
6. Cererile existente „Enable Shell” / „Allow package installs” raman neschimbate.

## Promptul

Agentii cu Shell primesc in system prompt o linie cu comenzile pe **Block** din regulile efective:
`These commands are blocked in this project; do not try them: sudo, ...`. Comenzile pe Ask nu apar (agentul le
poate incerca, iar utilizatorul decide). Lista intra in hash-ul promptului, deci o schimbare de Block porneste o
sesiune noua (acceptat, ca la orice schimbare de prompt).

## Date (migrarea 023)

```
command_rules
  project_id INTEGER NOT NULL   -- 0 = global
  prefix     TEXT NOT NULL
  mode       TEXT NOT NULL CHECK (mode IN ('allow','ask','block'))
  updated_at TEXT NOT NULL
  PRIMARY KEY (project_id, prefix)
command_grants                   -- Allow once
  id, project_id, agent_id (FK ON DELETE CASCADE), prefix, created_at, used_at (NULL = nefolosit)
access_requests + coloana prefix TEXT NOT NULL DEFAULT ''   -- '' = cerere de tool, ca azi
```

Regulile proiectului se sterg cu proiectul (stergere explicita in `DeleteProject`, `project_id` nu are FK din
cauza lui 0). Migrarea insereaza cele 4 reguli globale implicite.

## API

```
GET  /api/settings/commands                    -> [{prefix, mode}]  (globale)
PUT  /api/settings/commands  [{prefix, mode}]  -> 204  (inlocuieste lista globala; validare; 400 cu mesaj)
POST /api/settings/commands/reset              -> 204
GET  /api/projects/{id}/commands               -> {global: [...], project: [...], effective: [...], warnings: [...]}
PUT  /api/projects/{id}/commands [{prefix, mode}] -> 204 (doar override-urile proiectului)
POST /api/access/{id}/allow-once               -> 204
POST /api/access/{id}/allow-project            -> 204
POST /api/access/{id}/block-project            -> 204
(Dismiss existent ramane)
```

`warnings`: combinatiile Allow-sub-Ask/Block descrise mai sus, ca text.

## Rularea unui agent

`prompt.Settings` primeste regulile efective ale proiectului si grant-urile nefolosite ale agentului:
`deny = fixe + packageRules (fara Install packages) + Ask/Block (minus prefixele cu grant)`. Grant-urile folosite
se marcheaza la pornirea run-ului (`startRun`). `AlwaysDeniedBash` / `NeedsPackages` (folosite de cererile de acces)
iau in calcul regulile efective, nu lista fixa.

## UI

- **Settings** (`#/settings`, link nou in bara de sus, langa Projects | Library): sectiunea „Commands” — tabel
  Prefix (mono) / Allow · Ask · Block (comutator) / sterge, „+ Add command”, „Reset to defaults”, nota despre limita
  (prefixele se potrivesc dupa inceputul comenzii; un agent cu Shell le poate ocoli, de ex. cu `bash -c`). Salvare
  imediata. Pagina e gandita sa primeasca alte setari mai tarziu (sub-proiectul 4).
- **Setarile proiectului** (formularul existent): sectiunea „Commands” — regulile globale mostenite, fiecare cu
  select Inherit / Allow / Ask / Block, plus comenzile doar ale proiectului (+ Add); avertismentele din `warnings`.
- **Needs you:** cardul „COMMAND · ASK” cu comanda exacta (mono), agentul, T-<nr>, contorul si cele 4 butoane.
- **Ghidul** `docs/guide/safety.md` si `needs-you.md`: tabelul de reguli devine „implicit”, cu Settings si override.

## Teste

- Potrivire: cel mai lung prefix, cuvinte intregi, comenzi legate cu `&&`/`;`/`|`, override-ul proiectului peste
  global, prefix nou doar in proiect.
- `Settings()`: Ask si Block in deny ca `Bash(prefix:*)`, Allow scoate prefixul, grant-ul scoate prefixul, fixele raman.
- `recordDenials`: refuz pe Ask -> cerere cu prefix; pe Block -> fara cerere; agregare pe acelasi prefix.
- Allow once: grant consumat la run-ul urmator (al doilea run are iar prefixul in deny); agentul trezit; task-ul
  revine in coada. Allow in this project: regula scrisa, cererea inchisa.
- Validarea prefixelor (caractere interzise, lungime) si migrarea (cele 4 reguli implicite; `user_version` 23).
- API: fiecare ruta; avertismentele.

## Etape

1. Store + migrare: `command_rules`, `command_grants`, `access_requests.prefix`.
2. Potrivirea si `prompt.Settings` cu reguli efective si grant-uri; linia din prompt.
3. `recordDenials` + actiunile pe cerere (allow once / project / block) + trezirea agentului.
4. API Settings + proiect.
5. UI: Settings, sectiunea din proiect, cardul din Needs you; ghidul.
