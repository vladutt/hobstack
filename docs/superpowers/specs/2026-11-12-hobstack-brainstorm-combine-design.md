# Brainstorm: idei combinate si carduri noi

Data: 2026-11-12. Stare: design aprobat in discutie, de planificat. Pleaca de la Brainstorm (v1.0.10,
docs/superpowers/specs/2026-11-11-hobstack-brainstorm-design.md).

## De ce

Doua lucruri au iesit dupa prima folosire:

1. Doi agenti vin des cu idei care merg bine impreuna (de exemplu "host-ul e unul din jucatori" si "sesiuni asincrone").
   Utilizatorul vrea sa le combine intr-o singura idee, fara sa le rescrie el de la zero.
2. Cardurile de idei "arata ca naiba": azi fiecare card arata tot textul in markdown, deci in aceeasi runda au inaltimi
   foarte diferite si rundele devin un zid de text. E greu de vazut dintr-o privire ce idei sunt.

## Obiective

- Cardurile unei runde se pot citi dintr-o privire (varianta B din schite: tile-uri egale).
- Doua idei se pot combina; un agent scrie ideea combinata, iar utilizatorul o poate edita.
- Originalele raman in fir, marcate ca combinate.

## Ce nu e (non-goals)

- Nu se combina mai mult de doua idei odata (utilizatorul a cerut "doua idei intr-una singura"). O idee combinata se
  poate combina din nou cu alta.
- Intrebarile nu se combina, doar ideile (idea / wild) terminate.
- Agentii nu se citesc intre ei in runde; combinarea e singurul loc in care un agent vede ideea altuia, si o vede doar
  pentru ca utilizatorul i-a dat-o.

## Anti-goal

O idee combinata care e doar cele doua texte lipite una sub alta. Promptul cere o singura idee coerenta, care pastreaza
ce e bun din fiecare, rezolva conflictele dintre ele si spune ce a lasat deoparte.

## Ce a decis utilizatorul

- Combina un agent; utilizatorul poate edita rezultatul dupa.
- Originalele raman vizibile, marcate ca combinate.
- Designul cardurilor: varianta B din schite (.superpowers/brainstorm/46333-1794439036/content/cards-variants.html):
  tile-uri de aceeasi inaltime, culoarea agentului ca linie sus, autor + rol, titlu mare, 3 randuri din text, eticheta
  de stare jos; ideile wild cu nuanta mov, intrebarile teal cu butoanele de raspuns direct pe tile.
- Bifa pe tile + bara jos "N selected · Combine with <agent> ▾ · Clear · Combine".

## Comportament

### Tile-urile (design B)

- O runda = mesajul utilizatorului + un grid de tile-uri (4 pe rand pe ecran lat, 2 pe mediu, 1 pe telefon), toate de
  aceeasi inaltime pe rand.
- Pe tile: linia de sus in culoarea agentului (ui.toneOf), avatar + nume + rol, bifa (doar la ideile terminate), titlul
  (max 2 randuri), textul taiat la 3 randuri (fara markdown greu: textul simplu din markdown), jos etichetele:
  Picked, Wild, Merged, Combined, Edited, sau starea (Waiting, busy on T-42 / Thinking… / Stopped / Failed / Skipped).
- Wild: fundal cu nuanta mov (gradient discret), fara chenar punctat. Intrebare deschisa: fundal teal, optiunile ca
  butoane mici pe tile + Other (deschide modala cu camp de text). Intrebare cu raspuns: tile normal cu "You: <raspuns>".
- Card in asteptare / in lucru / esuat: tile estompat cu starea si butonul potrivit (Run now / Stop / Retry) direct pe tile.
- Click pe tile (in afara bifei si a butoanelor) deschide modala ideii: titlu, autor, textul complet in markdown,
  "Based on", si actiunile Pick / Unpick, Another idea, Retry, Edit (doar ideile combinate). Sagetile stanga / dreapta
  trec la tile-ul vecin din aceeasi runda, Esc inchide.
- Zona Picked de sus ramane, cu acelasi stil de tile, mai mic.

### Combinarea

1. Bifa apare pe ideile terminate (kind idea / wild, status done), din orice runda a subiectului, inclusiv pe cele
   combinate. Cu cel putin o bifa apare bara de jos (fixa la baza firului).
2. Cu exact 2 bifate, butonul Combine e activ; cu 1 sau peste 2 e dezactivat, cu explicatia "Select two ideas".
3. "Combine with <agent> ▾": implicit autorul primei idei bifate; lista = participantii subiectului.
4. Combine creeaza un card nou "waiting" pentru agentul ales, cu cele doua surse, si goleste bifele. Ruleaza ca orice
   card de brainstorming: cand agentul e liber, Run now trece inaintea cozii, Stop / Retry la fel.
5. Run-ul primeste in prompt cele doua idei complete (titlu, autor, text, based on) si cererea de a scrie o singura idee
   (vezi Anti-goal). Raspunde tot cu brainstorm_reply; kind poate fi doar idea sau wild (o intrebare e refuzata de tool
   pentru acest card, cu mesaj ca sa propuna).
6. Cardul combinat apare intr-un rand separat "Combined", direct sub runda care contine cea mai noua dintre cele doua
   surse, cu eticheta "Combined from: <titlu 1> + <titlu 2>" (titlurile sunt link: deruleaza la tile si il evidentiaza).
7. Cele doua surse primesc eticheta "Merged" (link spre ideea combinata). Pot fi in continuare alese (Pick), combinate
   din nou sau deschise. Daca ideea combinata esueaza, e oprita sau e combinata din nou, sursele raman "Merged" doar cat
   exista un card combinat terminat sau in lucru facut din ele.
8. Edit pe ideea combinata (in modala): titlu si text editabile; Save pastreaza textul utilizatorului si pune eticheta
   "Edited". Basis ramane cel al agentului. Edit nu porneste niciun run.
9. Ideea combinata intra in Save to Files si Send to lead ca orice idee aleasa, cu linia "Combined from: ...".

### Ce vede agentul in rundele urmatoare

Ideile combinate apar in context ca orice idee (alese complet, nealese doar titlul), cu "(combined from A + B)". Textul
editat de utilizator inlocuieste pe al agentului.

## Constrangeri

- Go + SQLite (migrare noua), UI vanilla JS embedded, live prin SSE (evenimentul "brainstorm" existent).
- Textele vazute de utilizator si prompturile in engleza; comentariile din cod in romana.
- Tema existenta (ardezie rece, Figtree, tokenii din app.css). Verificare cu capturi Chrome headless (lat si telefon).

## Decizii luate de mine (verificabile de utilizator)

- Se pot combina idei din runde diferite ale aceluiasi subiect.
- Doar ideile combinate se pot edita (ideile obisnuite raman cum le-a scris agentul).
- Modala ideii in loc sa se desfaca tile-ul in rand (rand-ul ramane egal).
- Agentul ales pentru combinare trebuie sa fie participant al subiectului.
- Max 2 surse; o idee combinata poate fi sursa pentru alta combinare.

## Decizii la verificare (tot ale mele)

- Run-ul de combinare are prompt propriu (nu BrainstormTurn cu mesajul rundei): numele si descrierea subiectului,
  atasamentele, ideile alese ale subiectului (ca context), cele doua surse complete si cererea de combinare. Fara
  "Another", fara intrebari (QuestionsLeft = 0, brainstorm_reply refuza kind question pe acest card).
- Run-ul de combinare NU reia sesiunea (subiect, agent): e un run fara --resume, fara sesiune salvata (doar citire,
  ca celelalte). Asa nu umple sesiunea agentului si nu depinde de ce a vazut el in runde.
- In contextul rundelor urmatoare sursele raman ca inainte (alese = complet, nealese = titlu in "do not repeat");
  ideea combinata apare in plus, cu "(combined from A + B)". Nu se ascunde nimic.
- Edit doar pe un card combinat cu status done. Nu atinge fisierul deja salvat (urmatorul Save to Files il suprascrie).
  Pe cardurile combinate nu exista "Another idea". Retry pe un combinat esuat / oprit pastreaza sursele.
- "Merged" se calculeaza la citire din sursele cardurilor combinate cu status done / waiting / running; nu se
  stocheaza. Aceeasi pereche se poate combina de mai multe ori; ordinea surselor nu conteaza (A = prima bifata).
  Sursele unei idei combinate care e la randul ei sursa raman Merged.
- Agentul care combina trebuie sa fie participant acum (altfel 400); scos ulterior = Skipped, ca orice card.
- Randul "Combined" se insereaza imediat dupa runda sursei mai noi (intre runde daca exista una mai noua); cardurile
  combinate nu apar in gridul rundei lor.

## Lasat celui care construieste

- Schema: de exemplu brainstorm_cards cu source_a / source_b (NULL pe cardurile normale) si edited; round_id al
  cardului combinat = runda sursei mai noi (NOT NULL ramane valabil). Numele exacte ale coloanelor si endpoint-urilor.
- Cum se aleg culorile de rol si plafonul de inaltime al tile-ului.
- Limitele de lungime pentru editare (aceleasi ca brainstorm_reply).
