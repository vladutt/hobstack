# Foreman: "Let AI draft this agent" (pagina Agent nou)

Data: 2026-10-08
Status: propunere, de aprobat (mockup-ul 05 din ../screenshots). Pagina Agent nou exista deja, fara aceasta caseta.

## Scop

Pe pagina Agent nou (`#/p/<id>/agents/new`), utilizatorul scrie ce lipseste din echipa ("cineva care sa se ocupe de
template-urile de e-mail") si primeste propuse: nume, rol, responsabilitati (inclusiv ce NU ia), model, sef,
reviewer si tool-uri. Le editeaza in formular inainte de "Add to team".

## Cum

- `POST /api/projects/{id}/agents/draft {ask, model}` porneste un run scurt `claude -p` (fara sesiune, fara
  `--resume`), doar citire, in folderul implicit al proiectului: acelasi mecanism ca analistul din wizard
  (`internal/setup`), dar cu un prompt mai mic si un singur tool MCP `propose_agent(...)` cu campurile formularului.
- Promptul primeste: scopul si contextul proiectului, echipa (nume, rol, responsabilitati, sef, reviewer), cererea.
  Regula: responsabilitatile nu se suprapun cu ale altor agenti; numele e unic si scurt.
- Raspunsul vine prin SSE ca la wizard (sau polling la 1 s, run-ul dureaza ~20-40 s); butonul "Draft" arata
  "Drafting..." si se poate opri.
- Formularul se completeaza doar in campurile goale sau neatinse de utilizator; un camp scris de mana nu se suprascrie.

## Limite

- Un singur draft activ per proiect; timeout 90 s; costul intra in "Total cost" al proiectului (run cu agent_id NULL?
  de decis: fie un tabel separat, fie un agent tehnic ascuns). Recomandare: `runs.agent_id` ramane obligatoriu, deci
  costul draft-ului se tine in `setup` ca la wizard si nu intra in total.

## Etape

1. Endpoint + run scurt reusing `internal/setup` (prompt nou, tool `propose_agent`).
2. Caseta din mockup ("Let AI draft this agent", camp + "Draft") si completarea formularului.
