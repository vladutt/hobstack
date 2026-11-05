# HobStack: agentul isi sterge fisierele lui fara sa intrebe

Data: 2026-11-05
Status: cerut de utilizator ("Daca un agent a facut un fisier si vrea sa-l stearga sa-l poata sterge fara probleme
daca este al lui / facut de el"), dupa un card "Canvas wants to run rm -rf .scratch" in Needs you.

## Decizia

- "Al lui" = creat de acel agent: in task_changes, kind create (Write pe un fisier nou sau un fisier nou prin shell,
  gitsnap), pe oricare task al lui (store.AgentCreatedPaths). Un folder e al lui daca tot ce e in el (max 5000
  fisiere) e creat de el; un folder gol, daca a avut fisiere create de el sau o comanda mkdir a lui il numeste.
  In plus: in folderul agentului (fara symlink-uri spre afara, nu folderul insusi) si neurmarit de git. O cale
  relativa care nu exista in folderul agentului (comanda a rulat dupa un cd) se cauta printre caile create de el:
  ".scratch" -> "site/app/.scratch" daca e singura potrivire.
- Fisierele scrise prin shell (heredoc, cat >) in afara git-ului nu apar in task_changes, deci nu sunt ale agentului:
  stergerea lor ramane la utilizator (sa le socotim ale lui dupa un mkdir ar putea prinde un folder al utilizatorului).
- Tool-ul MCP delete_files (paths): pe run-urile pe task ale agentilor care pot scrie. Sterge daca toate caile sunt ale
  lui, altfel nu sterge nimic si spune care nu e. Evenimentul deleted_files pe task.
- Plasa: un refuz Bash pe o comanda `rm` simpla (flag-uri -r/-f/-v, cai fara glob-uri, variabile sau alte comenzi)
  pe care regula Ask a oprit-o: daca exista ceva de sters si e tot al agentului, HobStack sterge si nu face card in
  Needs you. O regula Block ramane Block; orice cale straina ajunge la utilizator ca inainte.
- Promptul de sistem nu se schimba (altfel toti agentii ar porni sesiuni noi): descrierea tool-ului spune "use it
  instead of rm".
