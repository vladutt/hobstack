# Idee: chat general (asistent) pentru setup si modificari

Stare: idee salvata, brainstorming oprit inainte de design pe sectiuni. Nu exista spec sau plan.

## Ce vrea utilizatorul
Un chat disponibil pe orice pagina, care ajuta la setup si modifica lucruri la cerere.
Stie pe ce pagina esti ("schimba-i modelul in Sonnet" = agentul deschis).

## Decizii luate
- Scope: configuratie + actiuni. Configuratie: proiect, foldere, agenti si ierarhie/reviewer,
  etape, sarcini recurente, proiecte legate. Actiuni: creeaza task-uri, Start/Stop team,
  raspunde la intrebarile din "Needs you".
- Aplicare: propunere + Apply, apoi Undo. Nimic nu se schimba pana la Apply; dupa Apply,
  Undo anuleaza tot pachetul.
- UI: buton flotant in dreapta-jos, deschide un sertar lateral peste pagina. Pagina ramane
  vizibila si se actualizeaza la Apply.

## Abordarea recomandata (A, neconfirmata inca)
- Tool-uri MCP doar de citire (`get_projects`, `get_project`) + un singur tool de scriere
  `propose_changes` cu o lista de operatii dintr-un set inchis (update_project, add_agent,
  update_agent, remove_agent, set_stages, add_recurring, create_task, start_team,
  answer_question ...).
- Serverul valideaza; in chat apare cardul "Proposed" cu efectul fiecarei operatii
  ("Forge: model Opus -> Sonnet") si Apply / Drop.
- Apply ruleaza operatiile prin aceleasi functii din store/team ca rutele HTTP, intr-o
  tranzactie unde se poate.
- Undo: inainte de Apply se salveaza starea a ce se schimba; Undo o restaureaza doar daca
  nimeni nu a mai modificat aceleasi lucruri intre timp. Actiunile care nu se pot anula
  (ex. raspuns la o intrebare care a trezit agentul) sunt marcate "can't be undone".

Respinse: B (stare dorita ca la propose_setup: merge prost cu actiunile si cu mai multe
proiecte), C (tool-uri care blocheaza run-ul pana la Apply: fragil).

## Presupuneri de confirmat
- Contextul paginii: fiecare mesaj trimite ruta (proiect, tab, agent selectat, task deschis);
  serverul adauga "The user is on: ..." si un rezumat proaspat al proiectului.
- O singura conversatie globala, salvata in SQLite, cu "New chat".
- Poate citi folderele proiectului curent (`--add-dir`, doar citire); nu scrie fisiere.

## Cand reluam
Reia brainstorming-ul de la "confirma abordarea A si presupunerile", apoi design pe sectiuni,
spec in docs/superpowers/specs/, plan.
