# HobStack: coloanele proprii langa cele standard

Data: 2026-11-02
Status: cerut de utilizator ("coloanele langa cele standard"), fara intrebari; deciziile de mai jos sunt ale implementarii.

## Problema

Pana acum, cand un proiect avea macar o etapa (Columns), board-ul isi pierdea coloanele standard
(Queued / In progress / Review) si orice task nou intra in prima etapa. Pe Mobile-API utilizatorul a
adaugat coloana "Jira" (fara agenti) si coloanele standard au disparut; task-urile noi ar fi stat in Jira la nesfarsit.

## Decizia

Etapele sunt **opriri optionale**, nu drumul obligatoriu al fiecarui task.

- Board-ul are mereu Queued | In progress | <etapele proiectului> | Review | Done.
  Un task cu `stage_id` (queued / in_progress / blocked intr-o etapa) sta in coloana etapei; restul stau
  dupa status. "With lead" si "No stage" dispar: Review le tine pe toate cele care asteapta o decizie.
- Un task intra intr-o etapa doar explicit:
  - din board: formularul New task are la Assignee si optiunile "<etapa>: first free agent"; "+ Add a task"
    din coloana unei etape le preselecteaza; drag pe coloana etapei (exista deja);
  - liderul: `create_task` primeste `stage` (optional). Fara `stage` task-ul merge pe drumul standard
    (regula echipei directe ramane); cu `stage` intra in etapa aceea (orice lider sau radacina, asignatul
    optional si din etapa);
  - sarcinile recurente fara asignat (posibile doar cu etape) intra in prima etapa, ca inainte.
    Cele cu asignat merg pe drumul standard (asignatul nu mai trebuie sa fie in prima etapa).
  - cererile din proiecte legate merg la radacina, pe drumul standard.
- Din etapa, task-ul curge ca inainte: etapa urmatoare, dupa ultima Review la decident.
- `tasks.via_stages` (migrarea 033): task-ul a trecut prin etape. Send back (liderul, utilizatorul, drag pe Queued)
  intoarce in etapa doar task-urile cu via_stages; celelalte merg la asignat, ca fara etape. Tras pe Queued,
  un task dintr-o etapa iese din etape (via_stages = 0) si ramane la asignatul curent; un Done redeschis pe
  Queued la fel.
- Promptul liderului (stagesOverview) spune ca etapele sunt optionale si cum se pune un task in una.

## In afara

Fara schimbari in scheduler (alege deja dupa `stage_id`), in tipurile de etape sau in editorul de coloane.
