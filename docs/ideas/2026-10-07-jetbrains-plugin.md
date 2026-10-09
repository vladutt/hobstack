# Idee: plugin JetBrains pentru HobStack

Stare: idee, nimic implementat. Recomandare: dupa sabloanele de echipa si dupa trecerea prin
instalare (vezi 2026-10-07-company-admin.md, "Ordinea").

## Ce vrea utilizatorul
Sa gestioneze agentii proiectului direct din IDE, si sa deschida fisiere direct din plugin.

## Forma propusa: plugin subtire, UI-ul web refolosit
- Tool window cu browser JCEF care deschide http://127.0.0.1:7777 pe proiectul curent.
  Un singur UI: orice functionalitate noua apare automat si in IDE.
- Pluginul face doar ce browserul nu poate:
  1. Leaga proiectul din IDE de proiectul HobStack dupa calea folderului (endpoint nou pe server:
     "ce proiect contine folderul X"); daca nu exista, ofera "New project with AI" pe folder.
  2. Porneste HobStack daca nu ruleaza, sau arata unde se instaleaza.
  3. Integrari native (mai jos).
- Rezerva: daca JCEF e dezactivat, "Open in browser".
- Respins: UI nativ in Kotlin/Swing (dubleaza board, canvas, chat la fiecare functionalitate).

## Deschiderea fisierelor (podul JS <-> plugin)
- Pluginul expune in pagina `window.hobstackIDE` prin `JBCefJSQuery`
  (ex. `openFile(path, line)`, `openDiff(taskId, path)`).
- UI-ul web detecteaza `window.hobstackIDE`: in plugin caile devin linkuri "open in editor";
  in browser ramane butonul existent "Open in: Editor" (prin server).
- Ce se deschide:
  - fisierele din tab-ul Changes al unui task, la primul hunk (`FileEditorManager` / `OpenFileDescriptor`);
  - diff-ul unui task in diff viewer-ul IDE-ului (`DiffManager`), din datele task_changes;
  - caile din chat si din rapoarte (`path:line`);
  - folderul agentului selectat, in Project view.
- Sens invers, din IDE spre HobStack: "Send selection to agent" (cod + cale + linii in chat),
  "Ask about this file", "Create task from selection".
- De rezolvat: caile din task_changes sunt salvate cu numele folderului in fata
  ("LM - Dashboard/tests/..."); API-ul trebuie sa trimita si calea absoluta.
- Nu: deschidere automata a fisierului pe care il editeaza un agent (schimba taburile cat scrii);
  in loc, o lista discreta "Forge edits: X.php" cu click.
- Notificare IDE pentru "Needs you".

## De stiut
- Kotlin + Gradle + IntelliJ Platform SDK (limbaj nou pentru autor, dar putin cod).
- API-urile JetBrains se schimba intre versiuni: verificare la fiecare versiune majora.
- Concurenta in IDE: AI Assistant / Junie, pluginul oficial Claude Code. Pluginul trebuie sa arate
  ce e diferit (echipa, board, review intre agenti), nu inca un chat.

## Spike (o zi)
Tool window JCEF care deschide HobStack pe proiectul curent + click pe un fisier din Changes care
il deschide in editor la linia corecta.
