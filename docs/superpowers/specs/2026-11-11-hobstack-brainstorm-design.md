# Brainstorm: schimb de idei cu agentii echipei

Data: 2026-11-11. Stare: design aprobat in discutie, de planificat.

## De ce

Utilizatorul lucreaza la proiecte (exemplul de pornire: un joc) unde, inainte de orice task, vrea sa gandeasca
directia impreuna cu agentii: ce gameplay, ce upgrade tree, cum facem multiplayer-ul. Azi singurul loc e chat-ul cu
un agent, deci primeste o singura perspectiva, amestecata cu munca, si nimic nu ramane organizat.

Brainstorm e un loc separat de board: utilizatorul pune o intrebare pe un subiect, fiecare agent ales raspunde din
perspectiva lui (idee, idee "nebuna" sau intrebare), utilizatorul alege ce-i place, iar ce a ales ramane ca
informatie, ajunge in Files pe proiect sau merge la lider ca sa faca task-uri.

Nu e planning si nu porneste munca singur.

## Obiective

- Subiecte de brainstorming per proiect, ca niste canale; fiecare cu descriere, participanti, atasamente si un fir.
- La fiecare mesaj al utilizatorului, fiecare participant raspunde cu un card din perspectiva lui.
- Agentul poate raspunde cu o idee, o idee nebuna (marcata vizual diferit) sau cu o intrebare pentru utilizator.
- Utilizatorul alege ideile (Pick); cele alese stau fixate sus in subiect.
- Iesiri: doar informatie, fisier in Files pe proiect, sau trimis liderului ca sa faca task-uri.
- Raspunsurile pornesc din proiectul real: codul, documentele, descrierea subiectului si atasamentele.

## Ce nu e (non-goals)

- Agentii nu discuta intre ei si nu se citesc in aceeasi runda.
- Nu creeaza task-uri si nu modifica fisiere singur; task-urile apar doar cand utilizatorul trimite la lider.
- Nu e un planner pe pasi definiti de utilizator: pasii apar natural, cand agentii intreaba inainte sa propuna.
- Nu are persoane speciale de brainstorming ("wild card", "jucatorul critic"): participa agentii din echipa.

## Anti-goal (esec chiar daca merge tehnic)

Raspunsuri generice, din manual, rupte de proiect. Tot designul promptului si al contextului e facut ca sa evite asta:
agentul pleaca de la ce gaseste in proiect si in atasamente, spune pe ce se bazeaza, iar cand ii lipseste ceva
important intreaba in loc sa ghiceasca.

## Ce a decis utilizatorul

- Layout-ul F din schite: subiectele in stanga ca niste canale, firul in mijloc; la fiecare mesaj al utilizatorului
  vine un rand de carduri, cate unul de la fiecare participant; ideile alese fixate sus.
- Participa agentii din echipele proiectului, cu rolul si contextul lor.
- Participantii se aleg pe subiect (nu pe mesaj); se pot schimba ulterior.
- Un agent ocupat nu raspunde imediat: intrebarea asteapta pana cand agentul nu mai are nimic de lucru (nici run
  activ, nici task in coada pentru el). Brainstorming-ul nu intrerupe si nu intarzie munca.
- Agentii pot pune intrebari utilizatorului in loc sa vina direct cu o idee.
- Utilizatorul poate atasa fisiere si poze la subiect.
- Ce s-a ales poate: ramane doar ca informatie, intra in Files pe proiect, sau e trimis liderului sa-l faca task-uri.
- Pe un card care asteapta exista "Run now": agentul raspunde la brainstorming inaintea urmatorului task din coada
  (termina ce face acum, apoi ruleaza cardul). Fara "Run now", regula ramane "cand nu mai are nimic de lucru".
- Intrebarile agentilor NU apar in "Needs you"; raman in tab-ul Brainstorm.

## Comportament

### Subiect

Un subiect (topic) are: nume (scurt, unic in proiect), descriere libera scrisa de utilizator (ce vrea, constrangeri:
"co-op, buget mic de server, Godot"), lista de participanti (agenti ai proiectului, cel putin unul), atasamente
(fisiere si poze) si firul de runde. Subiectele se pot arhiva (dispar din lista, raman in baza).

### Runda

1. Utilizatorul scrie un mesaj in subiect (poate atasa si fisiere in acelasi pas, ca la task-uri: buton, drag & drop,
   Cmd+V cu imagini, prin paste.js).
2. Se creeaza o runda cu un card "waiting" pentru fiecare participant. Pe card: "Waiting, busy on T-42" cand agentul
   lucreaza, "Thinking…" cand ruleaza run-ul de brainstorming.
3. Cand un participant nu mai are nimic de lucru, primeste un run de brainstorming pentru cardul lui (vezi Executie).
4. Agentul raspunde cu exact un card: idee, idee nebuna sau intrebare.
5. Intrebare: cardul arata intrebarea cu 2-5 optiuni pe butoane plus "Other" (text liber), ca la Decide din task-uri.
   Raspunsul utilizatorului pune cardul inapoi in asteptare pentru acelasi agent; la urmatorul run agentul primeste
   raspunsul si vine cu ideea (sau, rar, cu inca o intrebare; limita: max 2 intrebari pe agent pe runda, apoi
   promptul ii cere sa propuna cu ce stie).
6. Pe un card de idee: Pick / Unpick. "Pick" il fixeaza in zona "Picked" din capul subiectului.
7. Pe un card se poate cere "Another idea" (agentul mai da o idee pe acelasi mesaj, fara sa repete ce a dat).
8. Pe un card in asteptare: "Run now" (vezi Executie).
9. Stop pe un run de brainstorming (Stop pe agent sau pe card) lasa cardul "Stopped" cu "Retry"; Reconcile nu-l
   reporneste singur (altfel agentul, liber, l-ar relua imediat).

Utilizatorul poate scrie un mesaj nou inainte sa raspunda toti; runda veche ramane cum e, cardurile ei inca in
asteptare isi continua drumul.

### Ce vede agentul (contextul)

- Ruleaza in folderul lui, cu celelalte foldere ale proiectului prin --add-dir (ca orice run), doar citire.
- Primeste: numele si descrierea subiectului, lista atasamentelor (cai pe disc, sa le poata citi, inclusiv pozele),
  ideile alese pana acum (complet), titlurile ideilor nealese din rundele anterioare (ca sa nu le repete; F nu are
  "Drop", deci nealese = toate cele fara Pick), raspunsurile utilizatorului la propriile lui intrebari din runda
  curenta, toate intrebarile si raspunsurile din rundele anterioare, si mesajul curent.
- Nu vede cardurile celorlalti agenti din runda curenta. Vede ce a dat el insusi in subiect (sa nu se repete).
- Promptul ii cere: sa porneasca de la proiect (citeste codul si documentele relevante), sa spuna pe scurt pe ce se
  bazeaza (fisiere, atasamente), sa vorbeasca din rolul lui, sa intrebe cand ii lipseste ceva care schimba ideea, si
  sa marcheze ca "wild" doar ideile indraznete, nu pe toate.

### Iesiri

- "Just keep it": implicit, nimic de facut; ideile alese raman in subiect.
- "Save to Files": scrie un markdown cu descrierea subiectului si ideile alese (titlu, autor, text, pe ce se bazeaza,
  plus raspunsurile la intrebari relevante) in Files pe proiect (folderul de fisiere al proiectului,
  `config.ProjectFilesDir`), sub `brainstorm/<nume-subiect>.md`. Salvarea repetata il suprascrie (e un rezumat al
  starii curente).
- "Send to lead": trimite liderul ales (implicit prima radacina a proiectului) un mesaj de chat din partea
  utilizatorului cu ideile alese si cererea de a le transforma in task-uri. Fiind mesaj al utilizatorului, liderul
  poate porni task-uri la fel ca dintr-un chat obisnuit (requestOf / start_task / create_task). Mesajul are un link
  inapoi spre subiect. Nu exista alt drum spre task-uri.

## Executie (cum ruleaza)

- Un run de brainstorming e un tip nou de run: fara task, doar citire (aceleasi tool-uri ca un run de chat,
  prompt.ReadOnlyTools, fara start_task), cu un singur tool MCP specific pentru a raspunde cu cardul (de exemplu
  `brainstorm_reply`: kind idea | wild | question, title, text, basis, options pentru question). Daca run-ul se termina
  fara sa cheme tool-ul, textul final al agentului devine cardul de tip idee; un run esuat lasa cardul in "Failed"
  cu "Retry".
- Sesiunea Claude: separata de chat-ul agentului, una per (subiect, agent), ca brainstorming-ul sa nu umple chat-ul
  agentului si chat-ul sa nu se scurga in brainstorming. "New chat" pe agent nu o atinge.
- Un singur run activ per agent ramane regula: un run de brainstorming ocupa agentul ca orice alt run; Stop pe agent il
  opreste (cardul revine in asteptare).
- Prioritate in Reconcile: dupa toate celelalte (decizii, task-uri, note nelivrate). Un agent primeste un run de
  brainstorming doar daca nu e activ, nu are task Queued asignat lui si nu are note nelivrate.
- "Run now" marcheaza cardul ca prioritar: cand agentul devine inactiv, cardul lui prioritar porneste inaintea
  urmatorului task din coada (dar dupa deciziile de la pasul 0 din Reconcile si dupa notele nelivrate, care sunt
  scurte si deblocheaza alti agenti). Nu opreste run-ul in curs.
- Mai multe carduri in asteptare pentru acelasi agent: in ordinea crearii (prioritarele intai).
- Un card in asteptare pentru un agent sters sau scos din participanti se inchide ca "Skipped".
- Costul run-urilor intra in Stats ca al oricarui run al agentului.

## Constrangeri

- Go + SQLite (migrare noua), UI vanilla JS embedded, live prin SSE (hub-ul existent), ca restul aplicatiei.
- Textele vazute de utilizator si prompturile in engleza; comentariile din cod in romana.
- Atasamentele: aceleasi limite ca la fisierele de pe task (50 MB / fisier, 200 MB / request, max 50 pe subiect),
  scrise prin staging + rename, citite de agent prin --add-dir pe folderul lor.

## Decizii luate de mine (verificabile de utilizator)

- Tab nou "Brainstorm" pe proiect, langa Board si Stats; ruta `#/p/<pid>/brainstorm[/<topic>]`.
- Run-urile de brainstorming sunt doar citire, ca run-urile de chat.
- Intrebarile agentului au butoane + "Other", ca Decide.
- Sesiune Claude separata per (subiect, agent).
- Pe pauza (Stop team) brainstorming-ul merge totusi, ca si chat-ul: e cerut direct de utilizator. Un agent pe pauza
  cu task-uri in coada e considerat liber (nu lucreaza).
- Max 2 intrebari pe agent pe runda.
- Stop pe un run de brainstorming = card "Stopped" cu Retry (nu se reia singur).
- Agentii nelegati (detached) pot participa: brainstorming-ul nu e munca. Un agent sters sau scos din participanti
  inchide cardurile lui in asteptare ca "Skipped".
- Full autonomy nu schimba nimic aici: agentii pot intreba (brainstorming-ul e pornit direct de utilizator).
- Tab-ul Brainstorm are o insigna cu numarul de intrebari ale agentilor fara raspuns (doar pe tab, nu in Needs you).
- "Save to Files" suprascrie acelasi fisier; "Send to lead" e un mesaj de chat obisnuit, nu un mecanism nou.
- Atasamentele stau in folderul de date al proiectului (langa task-files), nu in folderele de cod.

## Lasat celui care construieste

- Schema exacta a tabelelor (subiecte, participanti, runde, carduri, atasamente, sesiuni) si numele lor.
- Numele si forma exacta a tool-ului MCP de raspuns si a endpoint-urilor.
- Cum se integreaza tipul nou de run in Reconcile / startRun / finish fara sa dubleze logica de chat.
- Stilul vizual exact, in tema existenta (ardezie rece, Figtree); ideile wild cu chenar punctat mov ca in schite.
- Limitele de lungime pentru nume, descriere, mesaje si carduri.
