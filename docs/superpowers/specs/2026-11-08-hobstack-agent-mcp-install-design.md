# Agentii cer servere MCP pentru proiect (install_mcp)

## De ce
Un agent are nevoie de un server MCP (browser, baza de date, un API) si pana acum doar utilizatorul il putea
instala, din Library > MCP. Utilizatorul vrea ca agentul sa-l poata cere, doar pentru proiectul lui.

## Decizia utilizatorului
Orice instalare trece prin aprobarea lui in Needs you, si cu Full autonomy: un server local porneste un program
pe masina lui.

## Ce face
- Tool-ul MCP install_mcp (toti agentii, fara wizard / draft): name, description, command + args (local) sau url
  (remote), secrets (numele cheilor), reason. Validare ca in Library (library.ValidateInput); "<hidden>" si ${DIR}
  in comanda / url refuzate.
- mcp_requests (migrarea 036). Max 5 deschise pe proiect; o cerere deschisa cu acelasi nume nu se dubleaza; un
  server deja instalat in proiect = raspuns "already installed". Un nume care exista deja in Library = cererea
  activeaza elementul acela (item_id, comanda lui, nu a agentului).
- Needs you (mcp-requests.js): agentul, task-ul, descrierea, motivul, comanda exacta / URL-ul, campuri pentru chei
  (optionale; se pot pune si mai tarziu in Instructions > Library), Install / Decline. Install = acelasi drum ca
  Library > MCP > Add server (web.createMCP: proba tools/list pentru local, element publicat "You", instalat si
  activat doar in proiectul cererii); proba picata = mesajul + "Install without test" dupa confirmare.
- Agentul afla printr-o nota in coada (+ Kick): instalat (tool-urile mcp__<nume>__* de la urmatorul run) sau refuzat.
  Pe task: evenimentele mcp_requested / mcp_installed / mcp_declined.
- POST /api/mcp-requests/{id}/install {secretValues, skipTest} si /decline; projectCard.mcpRequests intra in
  "N need you".
