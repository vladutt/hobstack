# HobStack: update din aplicatie (din git)

Data: 2026-11-05
Status: cerut de utilizator ("un buton de update last version de pe git"); deciziile de mai jos sunt ale implementarii.

## Problema

HobStack ruleaza din folderul cu codul sursa (binarul `hobstack` e construit in radacina repo-ului, push pe
github.com/vladutt/hobstack). Pana acum update-ul insemna: git pull, go build, oprit, pornit, si grija sa nu
opresti agenti in lucru.

## Decizia

- Varianta din sursa (nu binare din GitHub Releases): merge doar cand HobStack ruleaza din folderul sursa
  (go.mod cu `module hobstack` si `.git` langa binar, sau HOBSTACK_SOURCE) si exista `git` si `go` in PATH.
  Altfel Settings > Version arata versiunea si motivul.
- Versiunea curenta = `vcs.revision` din binar (go build o pune singur, cu vcs.time si vcs.modified).
- Verificare: la 1 minut dupa pornire, apoi din ora in ora, si la "Check now": `git fetch` pe ramura urmarita
  (@{u}, de ex. hobstack/main), commit-urile noi = revizia binarului..@{u} (HEAD daca repo-ul nu are revizia).
  git ruleaza fara prompt-uri (GIT_TERMINAL_PROMPT=0, ssh BatchMode).
- Update (pachet internal/selfupdate):
  1. refuz daca sursa are modificari necommit-uite (fara fisierele neurmarite) sau commit-uri locale care nu
     sunt pe @{u} (ar cere merge);
  2. `git merge --ff-only <target>`, `go build -o <exe>.new ./cmd/hobstack` (10 min); la eroare sursa revine cu
     `git reset --keep <vechi>` si nimic nu se schimba (ultimele 20 de linii din build in UI);
  3. binarul vechi devine `<exe>.prev`, cel nou ia locul lui (rename, merge si pe Windows);
  4. Hold: echipele nu mai pornesc run-uri noi (Reconcile amana proiectul), se asteapta pana nu mai ruleaza
     niciun run (runner.ActiveCount si team.ActiveCount); "Cancel restart" lasa binarul nou pus, pentru
     urmatoarea pornire ("Restart now" nu il mai construieste o data);
  5. copia bazei de date (VACUUM INTO) in ~/.hobstack/backups, ultimele 3; daca nu merge, nu reporneste;
  6. restart: contextul aplicatiei se anuleaza, serverul se inchide, team.Drain asteapta finish-ul in curs,
     baza de date se inchide, apoi main porneste binarul nou cu aceleasi argumente plus --no-open
     (syscall.Exec pe Unix; proces nou pe Windows).
- API: GET /api/update, POST /api/update/check, POST /api/update, POST /api/update/cancel.
- UI (update.js): insigna in bara de sus ("Update · N", pasul curent, "Waiting for N agents", "Update failed");
  fereastra cu commit-urile (max 30), Update and restart / Check now / Cancel restart; sectiunea Version din Settings.
  Dupa restart pagina se reincarca singura cand serverul raspunde cu alta revizie.

## In afara

Binare gata facute (goreleaser / GitHub Releases), rollback din UI la `.prev`, update automat fara clic.
