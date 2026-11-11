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

## Completare (2026-11-11): instalare directa la cererea utilizatorului din chat

Un lider sau o radacina (canAddAgents) intr-un run pornit dintr-un mesaj al utilizatorului din chat (requestOf, ca la
create_recurring; Full autonomy nu ajunge) instaleaza serverul direct, fara card in Needs you, daca nu cere chei (valorile
lor le da doar utilizatorul). team.RequestMCP pregateste cererea sub t.work, apoi cheama, fara lock, instalatorul pus de
web (SetMCPInstaller -> Server.installForAgent): un nume din Library doar se activeaza in proiect, altfel createMCP cu
proba, ca la Library > MCP > Add server. Proba picata (team.MCPTestError) = cererea merge totusi in Needs you, cu motivul,
ca utilizatorul sa-l poata instala fara test; alte erori ajung la agent. Pe task: evenimentul mcp_installed cu liderul ca actor.
Elementul creat asa are MadeBy / MadeByAgentID = liderul si source_project_id = proiectul, setat dupa publicare
(store.SetLibrarySourceProject; inainte, Publish trateaza elementul ca pe unul al utilizatorului doar cu sursa 0).
Pagina item-ului din Library arata "Made by <agent> in <proiect>" (link spre proiect).
