# Foreman: aprobarea unei comenzi din "Needs you" tine pe tot task-ul

Data: 2026-10-09
Status: implementat (2026-10-09).

## Problema

Agentii ruleaza cu `--permission-mode dontAsk`; permisiunile se fixeaza la pornirea run-ului (`--settings` inline).
Un "da" in chat nu schimba nimic, iar aprobarea din "Needs you" nu ajunge mereu:

1. **Agent cu Shell, comanda pe Ask** (`git push`, `rm -rf`...): "Allow once" pune un grant consumat la *urmatorul run al
   agentului, de orice fel* (`TakeCommandGrants`). Daca acel run e pe alt task sau in chat, grant-ul se pierde si
   agentul e refuzat din nou pe task-ul pentru care ai aprobat.
2. **Agent fara Shell** (ex. tool-ul `git`, care permite doar status/diff/log/add/commit/checkout -b): un `git stash`
   refuzat da un card cu "Enable Shell for X" (orice comanda Bash, permanent) si Dismiss. Nu exista aprobare doar pentru
   comanda respectiva. In plus, `enableTool` (Enable Shell / Allow package installs) **nu trezeste agentul** si nu repune
   task-ul in coada: aprobi si nu se intampla nimic.
3. Agentul nu stie ca permisiunile vin din Foreman: cere aprobarea in chat sau prin `ask_user` (promptul `howAsk` spune
   chiar "a permission ... call ask_user") si sugereaza `.claude/settings.local.json` sau iesirea din "don't ask".

## Decizii luate cu utilizatorul

- O aprobare din "Needs you" trebuie sa fie suficienta: agentul e trezit si reincearca singur.
- Aprobarea e pe **prefixul comenzii** (ex. `git stash`), nu pe tot Shell-ul.
- Tine pe **tot task-ul**: toate run-urile agentului pe acel task. Un refuz din chat (run fara task) se aproba pentru
  chat, pana la "New chat".
- "Allow in this project" ramane drumul permanent (doar pentru agentii cu Shell, ca acum). Block si refuzurile fixe
  (deny-ul din `denyFor`, de ex. instalarile de pachete fara tool-ul `packages`) nu pot fi ocolite printr-o aprobare.

In afara scopului: aprobari in timpul run-ului (`--permission-prompt-tool`, sub-proiectul 3), cardul de aprobare in chat,
reguli Allow pe proiect pentru agentii fara Shell.

## 1. Date: `command_grants` pe task

Migrarea 029 (028 era deja luata de account_usage):

```sql
DROP INDEX command_grants_open;
DELETE FROM command_grants WHERE used_at IS NOT NULL;   -- grant-urile consumate nu mai au rost
ALTER TABLE command_grants DROP COLUMN used_at;
ALTER TABLE access_requests ADD COLUMN kind TEXT NOT NULL DEFAULT '';  -- 'ask' / 'command' / '' (cerere de tool)
UPDATE access_requests SET kind = 'ask' WHERE prefix <> '';
-- NULL = grant de chat (run-uri fara task), pana la New chat
ALTER TABLE command_grants ADD COLUMN task_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE;
CREATE INDEX command_grants_agent ON command_grants(agent_id, task_id);
```

Grant-urile nefolosite de dinainte devin grant-uri de chat (task_id NULL).

Store (`internal/store/commands.go`):
- `AddCommandGrant(projectID, agentID int64, taskID *int64, prefix string) error`: nu insereaza un duplicat
  (acelasi agent, task, prefix).
- `CommandGrants(agentID int64, taskID int64) ([]string, error)` inlocuieste `TakeCommandGrants`: intoarce prefixele
  grant-urilor agentului pentru task-ul run-ului (`task_id = ?`), sau cele de chat (`task_id IS NULL`) cand
  `taskID == 0`. Nu marcheaza nimic: grant-ul ramane valabil.
- `ClearChatGrants(agentID int64) error`: sterge grant-urile de chat; chemat din `StartNewChat` (aceeasi tranzactie).

Expirare: un grant pe task se aplica doar run-urilor pe acel task, deci dupa Done/arhivare nu mai are efect (nu mai
sunt run-uri); la Reopen e iar valabil, ceea ce e dorit. Nu e nevoie de filtru pe status. Stergerea task-ului (cascada)
sterge si grant-urile.

In `startRun` (`internal/team/run.go`): `granted := CommandGrants(a.ID, taskID)`; dispare logica de "pune la loc la
pornire esuata" (nu se mai consuma nimic).

## 2. Ce face un grant in permisiunile run-ului

`prompt.Settings` primeste un parametru nou `granted []string` (prefixe):
- fiecare prefix devine `Bash(<prefix>:*)` in **allow** (nou: asa merge si la agentii fara Shell; la cei cu Shell e
  redundant si inofensiv);
- `DenyPrefixes(rules, granted)` ramane: grant-ul scoate din deny doar un prefix Ask, niciodata un Block.

Deny castiga in Claude Code, deci Block si `denyFor` raman mai tari. Verificarea se face si la aprobare (sectiunea 4).

## 3. Prefixul unui refuz la un agent fara Shell

In `recordDenials`, pentru `d.Tool == "Bash"`:
- **cu Shell**: neschimbat (Block = doar eveniment; Ask = cerere pe prefixul regulii).
- **fara Shell** (nou): pentru fiecare bucata din `splitCommand(cmd)` care nu e deja permisa agentului (allow-ul din
  tool-urile lui + grant-urile curente):
  - `MatchCommand` pe bucata: Block = doar eveniment; Ask = cerere pe prefixul regulii (ca la Shell);
  - altfel, daca bucata e in refuzurile fixe (`AlwaysDeniedBash`, ex. `npm install` fara `packages`): cererea veche,
    fara prefix (cardul are deja "Allow package installs");
  - altfel: cerere pe prefixul calculat de `prompt.CommandPrefix(bucata)`.

`prompt.CommandPrefix(cmd string) string`: primul cuvant, plus subcomanda cand primul cuvant e o unealta cu
subcomenzi si al doilea cuvant arata a subcomanda (`^[a-z][a-z0-9:-]*$`). Unelte: `git npm npx pnpm yarn composer go
docker cargo make kubectl`, plus `php artisan` (doua cuvinte, apoi comanda artisan). Exemple:
- `git stash push -u -m "x" -- app/` -> `git stash`
- `ls -la` -> `ls`
- `php artisan migrate --force` -> `php artisan migrate`
- `git -C dir status` -> fara prefix (al doilea cuvant e flag; `git` ar insemna tot git-ul): ramane cererea de tool
  (Enable Shell). La fel interpretoarele si wrapper-ele (bash, sh, env, sudo, xargs, python, node, php fara artisan...).

Cererile se comaseaza ca acum (agent + tool + prefix, `count++`). O comanda compusa care are nevoie de doua prefixe da
doua carduri (rar). De verificat la implementare cu `claude` real: daca `cd <folder> && ...` produce un refuz pe `cd`;
daca da, `cd` intra in allow-ul fix (`Bash(cd:*)`), fiindca agentul oricum poate citi peste tot.

`accessView` primeste `HasShell bool` (cardul arata "Allow in this project" doar cu Shell).

## 4. Actiunile din "Needs you" si trezirea agentului

Team (`internal/team/commands.go`):
- `AllowCommandForTask(accessID)` inlocuieste `AllowCommandOnce`: `AddCommandGrant` cu `r.TaskID` (NULL = chat), apoi
  dismiss + `wakeForCommand` ca acum. Verificare la aprobare:
  - cerere pe un prefix Ask: regula e inca Ask (ca acum, altfel `ruleChangedError`);
  - cerere pe un prefix calculat: prefixul nu e acum Block si nu e in refuzurile fixe pentru tool-urile agentului
    (`AlwaysDeniedBash(prefix, a.Tools, rules)`), altfel `ruleChangedError`.
  - Alte cereri deschise ale aceluiasi agent cu acelasi prefix si acelasi task se inchid si ele.
- `AllowCommandInProject` / `BlockCommandInProject`: neschimbate, doar pentru cererile pe o regula Ask (un agent fara
  Shell nu primeste nimic dintr-o regula Allow, iar Block pe un prefix calculat nu e cerut); altfel 409 cu mesaj clar.
  Cererile Ask si cele calculate se deosebesc printr-o coloana noua `access_requests.kind` ('ask' / 'command', in
  migrarea 029; randurile vechi cu prefix sunt 'ask').
- `EnableTool(accessID, tool)` muta logica din `web.enableTool` in team si cheama `wakeForCommand` (cu un mesaj
  "You now have the Shell tool" / "...can install packages"): **repara bug-ul** in care agentul nu era trezit.

`wakeForCommand` ramane: repune task-ul in coada (aceleasi conditii ca acum) si lasa un mesaj nelivrat. Textul spune
cat tine: `You may now run commands starting with "git stash" on T-12 (allowed by the user). Run your command again.`
(in chat: `in this chat`).

API: `POST /api/access/{id}/allow-task` inlocuieste `allow-once` (singurul consumator e `access.js`).

UI (`static/js/access.js`), ambele feluri de card cu prefix arata la fel ("COMMAND · ASK" doar pentru regulile Ask,
"COMMAND" pentru prefixele calculate):
- **Allow `git stash` on T-12** (sau `in this chat` fara task), primar;
- Allow `git stash` in this project (doar `hasShell`);
- Block (doar pentru regulile Ask, ca acum);
- Enable Shell for X (doar fara Shell, ca optiune permanenta);
- Dismiss.

## 5. Promptul agentilor

O constanta noua `howDenied`, adaugata pentru toti agentii (in toate modurile, inclusiv full autonomy):

> Command and tool permissions are set in Foreman, not in Claude Code settings. If something is denied, do not ask the
> user to allow it and do not suggest editing settings files or changing the
> permission mode: the denial already appears in the user's Needs you list, and approving it wakes you up. Say in one
> line what was denied and why you need it, then stop; do not report that work as finished while you wait. When you are told it is
> now allowed, run it again.

Textul nu pomeneste ask_user (in modul autonom promptul nu are voie sa-l contina, `TestSystemAutonomy`); descrierea
tool-ului ask_user spune ca nu e pentru comenzi refuzate.

`howAsk` devine "If you need a decision or information that only the user can give..." (fara "a permission").

## 6. Task-uri blocate

`stuckKind` (`internal/team/stuck.go`), cazul In progress: un task cu o cerere de acces deschisa a asignatului pe acel
task nu e "orphan" (asteapta utilizatorul, care il vede deja in "Needs you"); altfel detectorul l-ar impinge si agentul
ar fi refuzat din nou. Store: `HasOpenAccessRequest(agentID, taskID int64) (bool, error)`.

## Teste

- store: migrarea 029 (grant-urile consumate dispar, cele nefolosite devin de chat); `AddCommandGrant` fara duplicate;
  `CommandGrants` pe task vs chat, fara consum; `StartNewChat` sterge grant-urile de chat.
- prompt: `CommandPrefix` (exemplele de mai sus, flag pe pozitia 2, `php artisan`); `Settings` cu grant pune
  `Bash(prefix:*)` in allow; un Block sau un deny fix ramane in deny chiar cu grant.
- team: refuz `git stash` la un agent cu doar `git` da o cerere pe prefixul `git stash` cu task-ul; `AllowCommandForTask`
  trezeste agentul, repune task-ul si urmatoarele doua run-uri pe task primesc grant-ul, iar un run pe alt task nu;
  aprobarea unui prefix devenit Block e refuzata; `EnableTool` trezeste agentul si repune task-ul; task cu cerere de
  acces deschisa nu e "stuck".
- web: `allow-task`, `allow-project` refuzat pentru agent fara Shell, `hasShell` in `accessView`.
- UI: verificat cu screenshot (cardul cu prefix calculat si cel Ask).

## Dupa review (2026-10-09)

- Cererile se comaseaza doar pe acelasi task (sau tot in chat); agentul e trezit dupa ultima aprobare de pe task.
- Enable Shell inchide toate cererile Bash de tool/comanda ale agentului; Allow in this project doar pentru agentii cu Shell.
- Verificat cu claude 2.1.295 (`dontAsk`, agent cu doar `git`): `pwd`, `ls`, `cat`, `head`, `tail`, `wc`, `sort`, `uniq`,
  `grep`, `echo`, `find` (si in pipe) si `cd` singur sunt permise de claude fara reguli, deci nu fac carduri.
  `cd sub && git ...` e refuzat chiar cu `Bash(cd:*)` in allow (cu Shell trece): ramane Enable Shell.
- Un task cu o cerere de acces deschisa asteapta ca la o intrebare: nu merge in review la un run oprit fara report_done si nu
  e repus in coada la Stop team / restart.
- Un prefix calculat se refuza la aprobare doar pe Block / refuz fix (o regula Ask o ridica grant-ul); prefixele trec prin
  NormalizePrefix; instalarile de pachete au mai multe variante in refuzul fix.
