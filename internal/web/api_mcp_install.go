package web

import (
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"foreman/internal/library"
	"foreman/internal/mcp"
	"foreman/internal/mcpregistry"
	"foreman/internal/store"
)

// mcpProbeTimeout: cat asteptam la Install un server MCP local (un `npx` / `uvx` poate descarca pachetul).
var mcpProbeTimeout = 30 * time.Second

const registryDownMsg = "The MCP registry is not reachable right now. Try again in a minute."

// mcpIndex: ce servere MCP are deja Library (pentru "In library" in rezultate).
type mcpIndex struct {
	names, commands, urls map[string]bool
}

// commandKey: comanda + argumentele, separate de un caracter care nu apare in argumente.
func commandKey(command string, args []string) string {
	return strings.Join(append([]string{strings.TrimSpace(command)}, args...), "\x00")
}

// mcpLibraryIndex citeste elementele mcp. ListLibraryItems sare peste cele declined / unpublished;
// numele lor sunt totusi luate (Install le da 409), dar nu apar in Library, deci nici aici.
func (s *Server) mcpLibraryIndex() (mcpIndex, error) {
	items, err := s.st.ListLibraryItems(store.KindMCP)
	if err != nil {
		return mcpIndex{}, err
	}
	ix := mcpIndex{names: map[string]bool{}, commands: map[string]bool{}, urls: map[string]bool{}}
	for _, it := range items {
		ix.names[it.Name] = true
		if it.URL != "" {
			ix.urls[it.URL] = true
		}
		if it.Command != "" {
			ix.commands[commandKey(it.Command, it.Args)] = true
		}
	}
	return ix, nil
}

// has: acelasi nume, sau aceeasi comanda+argumente, sau acelasi URL.
func (ix mcpIndex) has(name, command string, args []string, url string) bool {
	if ix.names[name] {
		return true
	}
	if url != "" && ix.urls[url] {
		return true
	}
	return command != "" && ix.commands[commandKey(command, args)]
}

type registryView struct {
	mcpregistry.Server
	InLibrary bool `json:"inLibrary"`
}

// searchRegistry: GET /api/library/registry?search=&cursor= — proxy catre registrul MCP (browserul ramane pe localhost).
func (s *Server) searchRegistry(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	q := r.URL.Query()
	servers, next, err := s.registry.Search(r.Context(), q.Get("search"), q.Get("cursor"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, registryDownMsg)
		return
	}
	ix, err := s.mcpLibraryIndex()
	if err != nil {
		fail(w, err)
		return
	}
	out := make([]registryView, 0, len(servers))
	for _, sv := range servers {
		// numele propus in Library e cel care s-ar lovi de un element existent
		in := ix.has(sv.SuggestedName, sv.Command, sv.Args, sv.URL)
		out = append(out, registryView{Server: sv, InLibrary: in})
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": out, "nextCursor": next})
}

type localView struct {
	mcpregistry.LocalServer
	InLibrary bool `json:"inLibrary"`
}

// localMCP: GET /api/library/local-mcp — serverele din configurarile Claude ale utilizatorului.
// LocalServer are doar numele variabilelor de mediu; valorile nu se citesc mai departe.
func (s *Server) localMCP(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	out := []localView{}
	home, err := s.home()
	if err != nil || home == "" {
		writeJSON(w, http.StatusOK, map[string]any{"servers": out}) // fara folder al utilizatorului: nimic de aratat
		return
	}
	ix, err := s.mcpLibraryIndex()
	if err != nil {
		fail(w, err)
		return
	}
	for _, ls := range mcpregistry.LocalServers(home) {
		in := ix.has(ls.Name, ls.Command, ls.Args, ls.URL) || ix.names[ls.SuggestedName]
		out = append(out, localView{LocalServer: ls, InLibrary: in})
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": out})
}

type mcpInstall struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Kind        string   `json:"kind"` // "local" | "remote"
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	URL         string   `json:"url"`
	Secrets     []string `json:"secrets"`
	SkipTest    bool     `json:"skipTest"`  // "Install without test", dupa confirmarea utilizatorului
	ProjectID   int64    `json:"projectId"` // 0 = doar in Library; altfel instalat si activat in proiect
}

// installMCP: POST /api/library/install-mcp. Validare -> nume liber -> proba (doar local, fara skipTest) ->
// element publicat facut de "You" -> optional instalat in proiect. Proba picata = 422 si nimic creat.
// Comanda a fost confirmata de utilizator in UI inainte de cerere.
func (s *Server) installMCP(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	var in mcpInstall
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	// un argument ascuns din configurarile Claude nu e valoarea reala: nu se instaleaza niciodata
	if strings.Contains(in.Command, mcpregistry.Hidden) || strings.Contains(in.URL, mcpregistry.Hidden) ||
		slices.ContainsFunc(in.Args, func(a string) bool { return strings.Contains(a, mcpregistry.Hidden) }) {
		fail(w, badRequest("this server has a credential in its command, arguments or URL; move it to env in your Claude config, then install"))
		return
	}
	switch in.Kind {
	case "local":
		if strings.TrimSpace(in.URL) != "" {
			fail(w, badRequest("a local server has a command, not a url"))
			return
		}
	case "remote":
		if strings.TrimSpace(in.Command) != "" || len(in.Args) > 0 {
			fail(w, badRequest("a remote server has a url, not a command"))
			return
		}
	default:
		fail(w, badRequest("kind: local or remote"))
		return
	}
	// aceeasi validare ca POST /api/library: comanda si argumentele vin din browser, nu ne bazam pe ele
	v, err := library.ValidateInput(mcp.LibraryInput{Kind: store.KindMCP, Name: in.Name, Description: in.Description,
		Command: in.Command, URL: in.URL, Args: in.Args, Secrets: in.Secrets}, true)
	if err != nil {
		fail(w, badRequest(err.Error()))
		return
	}
	if (in.Kind == "remote") != (v.URL != "") {
		fail(w, badRequest("an mcp needs command (local) or url (remote)"))
		return
	}
	if in.ProjectID != 0 {
		if _, err := s.st.GetProject(in.ProjectID); err != nil {
			fail(w, err)
			return
		}
	}
	conflict := func() { writeErr(w, http.StatusConflict, s.lib.NameTakenMsg(v.Kind, v.Name)) }
	if _, err := s.st.FindLibraryItem(v.Kind, v.Name); err == nil {
		conflict()
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		fail(w, err)
		return
	}

	// proba, inainte de a crea ceva. Mediul: secretele date deja in proiect pentru acest element; elementul e nou
	// (numele e liber), deci nu are inca valori — proba porneste fara ele (ca Publish azi).
	// Remote: Publish nu are proba pentru remote, deci nici aici.
	var tools []string
	if in.Kind == "local" && !in.SkipTest {
		names, err := library.ProbeMCP(v.Command, v.Args, map[string]string{}, mcpProbeTimeout)
		if err != nil {
			msg := strings.TrimSpace(err.Error())
			if r := []rune(msg); len(r) > 300 {
				msg = string(r[:300]) + "..."
			}
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error": "The server did not answer tools/list: " + msg, "testFailed": true})
			return
		}
		tools = names
	}

	staging, err := s.lib.NewStaging()
	if err != nil {
		fail(w, err)
		return
	}
	defer os.RemoveAll(staging) // dupa AdoptStaged nu mai exista

	it := store.LibraryItem{Kind: v.Kind, Name: v.Name, Description: v.Description, MadeBy: "You",
		Secrets: v.Secrets, Command: v.Command, Args: v.Args, URL: v.URL, Transport: in.Kind, ToolNames: tools,
		// ascuns (declined) cat se pregateste; PublishSkipTest il face published (proba s-a facut mai sus)
		Status: store.ItemDeclined}
	if err := s.st.AddLibraryItem(&it); errors.Is(err, store.ErrConflict) {
		conflict()
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	undo := func() {
		_ = s.st.DeleteLibraryItem(it.ID)
		s.lib.DiscardProposal(it.ID)
	}
	if _, err := s.lib.AdoptStaged(staging, it.ID); err != nil {
		undo()
		fail(w, err)
		return
	}
	ver, err := s.lib.PublishSkipTest(it.ID, "")
	if err != nil {
		undo()
		libraryErr(w, err)
		return
	}
	if in.ProjectID != 0 {
		// acelasi drum ca PUT /api/projects/{id}/library/{item} cu installed=true: versiunea curenta, activat
		if err := s.st.Install(in.ProjectID, it.ID, ver.ID); err != nil {
			fail(w, err)
			return
		}
		s.hub.Publish(in.ProjectID, "project", map[string]int64{"id": in.ProjectID})
	}
	cur, err := s.st.GetLibraryItem(it.ID)
	if err != nil {
		fail(w, err)
		return
	}
	installs := 0
	if in.ProjectID != 0 {
		installs = 1
	}
	view, err := s.itemViewOf(cur, installs)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}
