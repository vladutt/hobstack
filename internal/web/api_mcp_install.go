package web

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"hobstack/internal/library"
	"hobstack/internal/mcp"
	"hobstack/internal/mcpregistry"
	"hobstack/internal/store"
)

// mcpProbeTimeout: cat asteptam la Install un server MCP local (un `npx` / `uvx` poate descarca pachetul).
var mcpProbeTimeout = 30 * time.Second

const registryDownMsg = "The MCP registry is slow or not reachable right now. Try again in a minute."

const catalogReadMsg = "The local MCP catalog cannot be read right now. Try again in a minute."

// mcpIndex: ce servere MCP are deja Library (pentru "In library" in rezultate).
type mcpIndex struct {
	names, commands, urls map[string]bool
}

// commandKey: comanda + argumentele, separate de un caracter care nu apare in argumente.
func commandKey(command string, args []string) string {
	return strings.Join(append([]string{strings.TrimSpace(command)}, args...), "\x00")
}

// mcpLibraryIndex citeste elementele mcp in orice stare: si cele declined / unpublished au numele luat
// (Install le da 409), deci apar "In library", consecvent cu 409.
func (s *Server) mcpLibraryIndex() (mcpIndex, error) {
	items, err := s.st.ListLibraryItemsAnyStatus(store.KindMCP)
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

// catalogStatus: starea catalogului local, nil (null in JSON) cand nu exista catalog.
func (s *Server) catalogStatus() *mcpregistry.CatalogStatus {
	if s.catalog == nil {
		return nil
	}
	st := s.catalog.Status()
	return &st
}

// searchRegistry: GET /api/library/registry?search=&cursor= — din catalogul local cand e gata, altfel proxy
// catre registrul MCP (browserul ramane pe localhost). Raspunsul are si starea catalogului.
func (s *Server) searchRegistry(w http.ResponseWriter, r *http.Request) {
	if !s.libraryOK(w) {
		return
	}
	q := r.URL.Query()
	var servers []mcpregistry.Server
	var next string
	var err error
	if s.catalog != nil && s.catalog.Ready() {
		servers, next, err = s.catalog.Search(q.Get("search"), q.Get("cursor"))
		if errors.Is(err, mcpregistry.ErrBadCursor) {
			// cu starea catalogului, ca UI-ul sa poata reincarca de la prima pagina
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid cursor.", "catalog": s.catalogStatus()})
			return
		}
		if err != nil {
			// fisierul catalogului nu se poate citi (spec: singurul 502 cu catalogul gata)
			log.Printf("mcp catalog: search: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": catalogReadMsg, "catalog": s.catalogStatus()})
			return
		}
	} else {
		servers, next, err = s.registry.Search(r.Context(), q.Get("search"), q.Get("cursor"))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": registryDownMsg, "catalog": s.catalogStatus()})
			return
		}
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
	writeJSON(w, http.StatusOK, map[string]any{"servers": out, "nextCursor": next, "catalog": s.catalogStatus()})
}

// registryStatus: GET /api/library/registry/status — starea catalogului (null fara catalog).
func (s *Server) registryStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"catalog": s.catalogStatus()})
}

// syncRegistry: POST /api/library/registry/sync — "Update now". Sincronizarea ruleaza in fundal pe contextul
// aplicatiei, nu pe al cererii (altfel s-ar opri cand raspundem).
func (s *Server) syncRegistry(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeErr(w, http.StatusNotFound, "The MCP catalog is not available.")
		return
	}
	st := s.catalog.Status()
	if st.Syncing {
		writeErr(w, http.StatusConflict, "The catalog is already updating.")
		return
	}
	ctx := s.catalogCtx
	if ctx == nil {
		ctx = context.Background()
	}
	cat := s.catalog
	go func() {
		// ErrSyncRunning (a pornit alta intre timp) nu e o problema; celelalte erori sunt salvate in catalog (last_error)
		if err := cat.Sync(ctx); err != nil && !errors.Is(err, mcpregistry.ErrSyncRunning) && ctx.Err() == nil {
			log.Printf("mcp catalog: sync: %v", err)
		}
	}()
	// goroutine-ul poate sa nu fi pornit inca: raspunsul spune deja ca se actualizeaza
	st.Syncing = true
	writeJSON(w, http.StatusAccepted, map[string]any{"catalog": st})
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
	// SecretValues: valorile secretelor pentru proiect (doar cu projectId). Intra in mediul probei si se salveaza
	// ca secretele proiectului; nu apar niciodata intr-un raspuns.
	SecretValues map[string]string `json:"secretValues"`
	// AgentID / AgentName: install_mcp direct al unui lider (installForAgent); elementul apare "made by" el.
	// Nu vin din browser. Gol = facut de utilizator ("You").
	AgentID   int64  `json:"-"`
	AgentName string `json:"-"`
}

// hasMarker: textul contine un marcaj care nu e o valoare reala.
func hasMarker(in mcpInstall, marker string) bool {
	return strings.Contains(in.Command, marker) || strings.Contains(in.URL, marker) ||
		slices.ContainsFunc(in.Args, func(a string) bool { return strings.Contains(a, marker) })
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
	cur, ferr := s.createMCP(in)
	if ferr != nil {
		ferr.write(w)
		return
	}
	// elementul e nou: instalat cel mult in proiectul cerut acum (fara o interogare in plus)
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

// mcpFail: de ce nu s-a putut instala (raspunsul HTTP, comun pentru Library si cererile agentilor)
type mcpFail struct {
	err        error // eroare obisnuita (fail)
	libErr     error // eroare de publicare (libraryErr)
	status     int   // altfel: cod + mesaj
	msg        string
	testFailed bool // proba n-a raspuns la tools/list (422): UI-ul ofera "Install without test"
}

func (f *mcpFail) write(w http.ResponseWriter) {
	switch {
	case f.testFailed:
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": f.msg, "testFailed": true})
	case f.libErr != nil:
		libraryErr(w, f.libErr)
	case f.err != nil:
		fail(w, f.err)
	default:
		writeErr(w, f.status, f.msg)
	}
}

// createMCP: validare -> nume liber -> proba (doar local, fara skipTest) -> element publicat facut de "You" (sau de
// liderul din install_mcp) -> optional instalat in proiect. Proba picata = testFailed si nimic creat. Comanda a fost confirmata de utilizator.
func (s *Server) createMCP(in mcpInstall) (store.LibraryItem, *mcpFail) {
	bad := func(msg string) (store.LibraryItem, *mcpFail) {
		return store.LibraryItem{}, &mcpFail{err: badRequest(msg)}
	}
	// un argument ascuns din configurarile Claude nu e valoarea reala: nu se instaleaza niciodata
	if hasMarker(in, mcpregistry.Hidden) {
		return bad("this server has a credential in its command, arguments or URL; move it to env in your Claude config, then install")
	}
	// ${DIR} e folderul fisierelor unei propuneri de agent; un server instalat de aici nu are fisiere
	if hasMarker(in, "${DIR}") {
		return bad("${DIR} only works for items with files; use the full path")
	}
	switch in.Kind {
	case "local":
		if strings.TrimSpace(in.URL) != "" {
			return bad("a local server has a command, not a url")
		}
	case "remote":
		if strings.TrimSpace(in.Command) != "" || len(in.Args) > 0 {
			return bad("a remote server has a url, not a command")
		}
	default:
		return bad("kind: local or remote")
	}
	// aceeasi validare ca POST /api/library: comanda si argumentele vin din browser, nu ne bazam pe ele
	v, err := library.ValidateInput(mcp.LibraryInput{Kind: store.KindMCP, Name: in.Name, Description: in.Description,
		Command: in.Command, URL: in.URL, Args: in.Args, Secrets: in.Secrets}, true)
	if err != nil {
		return bad(err.Error())
	}
	if (in.Kind == "remote") != (v.URL != "") {
		return bad("an mcp needs command (local) or url (remote)")
	}
	if len(in.SecretValues) > 0 && in.ProjectID == 0 {
		return bad("secretValues need projectId: secret values are kept per project")
	}
	if err := checkSecretValues(v.Secrets, in.SecretValues, nil); err != nil {
		return store.LibraryItem{}, &mcpFail{err: err}
	}
	if in.ProjectID != 0 {
		if _, err := s.st.GetProject(in.ProjectID); err != nil {
			return store.LibraryItem{}, &mcpFail{err: err}
		}
	}
	conflict := &mcpFail{status: http.StatusConflict, msg: s.lib.NameTakenMsg(v.Kind, v.Name)}
	if _, err := s.st.FindLibraryItem(v.Kind, v.Name); err == nil {
		return store.LibraryItem{}, conflict
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.LibraryItem{}, &mcpFail{err: err}
	}

	// proba, inainte de a crea ceva, cu valorile secretelor date acum (elementul e nou, deci in proiect nu are altele).
	// Remote: Publish nu are proba pentru remote, deci nici aici.
	env := map[string]string{}
	for k, val := range in.SecretValues {
		if val != "" {
			env[k] = val
		}
	}
	var tools []string
	if in.Kind == "local" && !in.SkipTest {
		names, err := library.ProbeMCP(v.Command, v.Args, env, mcpProbeTimeout)
		if err != nil {
			msg := strings.TrimSpace(err.Error())
			// serverul poate repeta valoarea unui secret in eroare: nu iese din server
			for _, val := range env {
				msg = strings.ReplaceAll(msg, val, mcpregistry.Hidden)
			}
			if r := []rune(msg); len(r) > 300 {
				msg = string(r[:300]) + "..."
			}
			return store.LibraryItem{}, &mcpFail{testFailed: true, msg: "The server did not answer tools/list: " + msg}
		}
		tools = names
	}

	staging, err := s.lib.NewStaging()
	if err != nil {
		return store.LibraryItem{}, &mcpFail{err: err}
	}
	defer os.RemoveAll(staging) // dupa AdoptStaged nu mai exista

	madeBy := "You"
	if in.AgentName != "" {
		madeBy = in.AgentName
	}
	it := store.LibraryItem{Kind: v.Kind, Name: v.Name, Description: v.Description, MadeBy: madeBy, MadeByAgentID: in.AgentID,
		Secrets: v.Secrets, Command: v.Command, Args: v.Args, URL: v.URL, Transport: in.Kind, ToolNames: tools,
		// ascuns (declined) cat se pregateste; PublishSkipTest il face published (proba s-a facut mai sus)
		Status: store.ItemDeclined}
	if err := s.st.AddLibraryItem(&it); errors.Is(err, store.ErrConflict) {
		return store.LibraryItem{}, conflict
	} else if err != nil {
		return store.LibraryItem{}, &mcpFail{err: err}
	}
	// undo sterge tot ce a facut Install: randurile (versiunile si instalarile cad prin cascade), propunerea si
	// folderul versiunii (items/<id>, mutat acolo de publicare)
	undo := func() {
		_ = s.st.DeleteLibraryItem(it.ID)
		s.lib.DiscardProposal(it.ID)
		_ = os.RemoveAll(filepath.Join(s.lib.Root, "items", strconv.FormatInt(it.ID, 10)))
	}
	if _, err := s.lib.AdoptStaged(staging, it.ID); err != nil {
		undo()
		return store.LibraryItem{}, &mcpFail{err: err}
	}
	ver, err := s.lib.PublishSkipTest(it.ID, "")
	if err != nil {
		undo()
		return store.LibraryItem{}, &mcpFail{libErr: err}
	}
	if in.ProjectID != 0 {
		// acelasi drum ca PUT /api/projects/{id}/library/{item} cu installed=true: versiunea curenta, activat,
		// plus secretele, intr-o tranzactie. La eroare elementul dispare de tot (altfel reincercarea ar da 409).
		if err := s.installInProject(in.ProjectID, it.ID, ver.ID, in.SecretValues); err != nil {
			undo()
			return store.LibraryItem{}, &mcpFail{err: err}
		}
		// facut de un lider: Library arata si proiectul (dupa publicare, ca Publish sa-l trateze ca pe al utilizatorului)
		if in.AgentID != 0 {
			if err := s.st.SetLibrarySourceProject(it.ID, in.ProjectID); err != nil {
				undo()
				return store.LibraryItem{}, &mcpFail{err: err}
			}
		}
		s.hub.Publish(in.ProjectID, "project", map[string]int64{"id": in.ProjectID})
	}
	cur, err := s.st.GetLibraryItem(it.ID)
	if err != nil {
		return store.LibraryItem{}, &mcpFail{err: err}
	}
	return cur, nil
}

// installWithSecrets: instalarea in proiect + secretele, intr-o tranzactie. Variabila exista ca testele sa poata
// simula o eroare (si sa verifice ca Install nu lasa jumatati de stare).
var installWithSecrets = (*store.Store).InstallWithSecrets
