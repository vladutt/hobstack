# Idee: modul Turbo (task-uri in paralel, cu worktree-uri git)

Stare: idee, nimic implementat. Discutata pe 2026-11-18.

## Ce vrea utilizatorul
Task-uri pornite in paralel, cu mai multi agenti lucrand in acelasi timp. Echipa se poate mari in Turbo; agentii
din Turbo "dorm" cat nu e nevoie de ei. Cand munca unora trece peste a altora: workspace-uri pe git, iar la final
totul adus pe un singur branch.

De ce acum: un agent are un singur run activ, deci un lider care da 4 task-uri aceluiasi agent (ex. Atlas ->
Nova, T-100..T-103) le vede facute pe rand.

## Forma propusa
- Clone, nu agenti noi: un agent primeste "Turbo: pana la N". Cand are mai multe task-uri in coada, pornesc copii
  ale lui (acelasi rol, instructiuni, tool-uri), fiecare pe alt task. Pe canvas ramane un nod ("Nova x3").
  "Sleep" e gratis: un agent fara run nu costa nimic (claude -p ruleaza doar la cerere). Clonele dispar cand se
  goleste coada.
- Worktree mereu in Turbo, nu doar la suprapunere: nu stii dinainte ce fisiere atinge un task, iar dupa e prea
  tarziu (gitsnap / Changes se amesteca deja azi cand doi agenti scriu in acelasi folder). Fiecare run paralel are
  worktree-ul si branch-ul lui (hobstack/T-101), din HEAD.
- Merge la fiecare task acceptat (Done), nu la final: HobStack aduce branch-ul pe branch-ul folderului. Conflict =
  task-ul se intoarce la clona ("rebase pe main si rezolva"); daca nu iese, Needs you.

## Ce e greu
- Tot ce presupune "un agent = un folder": Changes, modala de fisier, explorer, @, gitsnap, Files. Acum fiecare
  run are folderul lui (cea mai mare parte din munca).
- Ce nu e in git nu e in worktree: .env, node_modules, vendor. Trebuie un pas de pregatire (copiere / symlink /
  install); serverele de dev se bat pe porturi.
- Modificarile necommise din folderul principal nu se vad in worktree: in Turbo HobStack face commit la finalul
  fiecarui run.
- Dependente intre task-uri (T-101 dupa T-100): create_task ar avea nevoie de after: "T-100".
- Limitele contului: fereastra de 5 ore se consuma de N ori mai repede; plafon pe N si avertisment pe Stats.
- Doar pentru foldere git; fara repo, Turbo nu e disponibil.

## Intrebare deschisa
"Mai multi agenti pe acelasi lucru" poate insemna:
(a) task-uri diferite in paralel (forma de mai sus), sau
(b) N agenti pe acelasi task, cu variante concurente din care alegi una (ca Brainstorm, pentru cod).
Recomandare: (a) intai, (b) eventual mai tarziu.
