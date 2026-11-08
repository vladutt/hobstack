# Fisierele vizuale ajung la utilizator (capturi, imagini, SVG, PDF)

## De ce
Agentii fac capturi de ecran (teste, browser), SVG-uri, imagini si exporturi, dar utilizatorul le vedea doar daca
agentul le punea la report_done. Vrea sa le poata verifica: pe task sau in chat, cu Open si Show in Finder / Explorer.

## Ce face
- Automat, la finalul fiecarui run pe un task (team.attachVisuals, din finish, inainte de t.work): fisierele create
  sau schimbate in run (Write / Edit si prin shell, din gitsnap) cu extensia png, jpg, jpeg, gif, webp, avif, svg, pdf
  se ataseaza pe task (max 10 pe run, max 10 MB fiecare). Fisierele ignorate de git nu se vad prin shell; pentru ele
  e tool-ul.
- Tool-ul MCP attach_files (paths, max 20; note doar in chat), pentru toti agentii (fara wizard / draft):
  - run pe task: task_files ca la report_done files, pentru oricine lucreaza pe task-ul run-ului (si reviewer-ul);
  - run fara task (chat): copiile merg la fisierele proiectului ca atasamente "message" si apar intr-un mesaj al
    agentului (textul = note sau "Attached <nume>").
- Fara dubluri: un fisier cu aceeasi cale originala, aceeasi marime si nemodificat de la atasare se sare
  ("Already attached, unchanged"); unul schimbat intra ca versiune noua.
- Regula howVisuals in "How you work" pentru toti agentii: ce se poate vedea se ataseaza cu attach_files; capturile
  si exporturile stau in folderul agentului, nu in /tmp.
- task_files.source si attachments.source (migrarea 035): calea originala, absoluta.
- POST /api/task-files/{id}/open|reveal si /api/files/{id}/open|reveal: originalul cat exista, altfel copia
  (raspunsul spune original true/false). Open doar pentru tipuri care nu pornesc nimic (imagini, PDF, text, office,
  media, html); restul doar Reveal. Reveal: open -R (macOS), explorer /select, (Windows), xdg-open pe folder (Linux).
- UI: Open / Show in Finder (Explorer / folder) pe randurile din Files si in preview (task-files.js), sub atasamentele
  din chat (workspace.js attachmentNode); static/js/open-file.js. SVG-ul are preview ca <img> (script-urile lui nu
  ruleaza acolo); HTML ramane fara preview.

## Decizii
- Copia ramane (snapshot), ca la report_done: fisierul din proiect se poate schimba sau sterge.
- In chat nu e atasare automata: run-urile fara task nu modifica fisiere.
