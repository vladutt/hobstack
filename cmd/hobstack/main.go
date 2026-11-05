package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"hobstack/internal/claudecli"
	"hobstack/internal/config"
	"hobstack/internal/draft"
	"hobstack/internal/events"
	"hobstack/internal/library"
	"hobstack/internal/mcp"
	"hobstack/internal/mcpregistry"
	"hobstack/internal/opener"
	"hobstack/internal/picker"
	"hobstack/internal/runner"
	"hobstack/internal/selfupdate"
	"hobstack/internal/setup"
	"hobstack/internal/skillsrc"
	"hobstack/internal/store"
	"hobstack/internal/team"
	"hobstack/internal/web"
)

const usage = `%[1]s - local Claude Code agent teams

Usage:
  %[1]s start [--port 7777] [--no-open]
  %[1]s version
`

func main() {
	// fara comanda (dublu-clic pe Windows / macOS) sau doar cu flag-uri: e "start"
	if len(os.Args) < 2 || strings.HasPrefix(os.Args[1], "-") && os.Args[1] != "--version" && os.Args[1] != "-v" {
		os.Args = append([]string{os.Args[0], "start"}, os.Args[1:]...)
	}
	// pe Windows, pornit cu dublu-clic sau la login: fara fereastra de consola; log-ul merge in fisier
	background := os.Args[1] == "start" && ownConsole()
	if background {
		if err := logToFile(); err != nil {
			messageBox("HobStack cannot write its log: " + err.Error())
		}
		hideConsole()
	}
	var err error
	switch os.Args[1] {
	case "start":
		err = start(os.Args[2:])
	case "version", "--version", "-v":
		// update-ul ruleaza si el comanda asta pe binarul descarcat, ca proba ca porneste pe masina asta
		fmt.Println(config.Version)
		return
	default:
		fmt.Printf(usage, config.AppName)
		os.Exit(1)
	}
	var re restartError
	if errors.As(err, &re) {
		// dupa update: portul si baza de date sunt inchise; pornim binarul nou fara sa redeschidem browserul
		args := os.Args[1:]
		if !slices.Contains(args, "--no-open") && !slices.Contains(args, "-no-open") {
			args = append(args, "--no-open")
		}
		log.Printf("starting the new version")
		err = selfupdate.Reexec(re.exe, args)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if background {
			messageBox("HobStack could not start:\n\n" + err.Error())
		}
		os.Exit(1)
	}
}

// logToFile trimite log-ul (si stdout / stderr) in ~/.hobstack/hobstack.log; peste 10 MB il muta in .1.
func logToFile() error {
	paths, err := config.Default()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.Root, 0o755); err != nil {
		return err
	}
	path := filepath.Join(paths.Root, config.AppName+".log")
	if info, err := os.Stat(path); err == nil && info.Size() > 10<<20 {
		os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	log.SetOutput(f)
	os.Stdout, os.Stderr = f, f
	return nil
}

// alreadyRunning: pe adresa raspunde deja un HobStack (pornit a doua oara, de ex. dublu-clic sau la login).
func alreadyRunning(addr string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://" + addr + "/api/info")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var info struct {
		Name string `json:"name"`
	}
	return json.NewDecoder(resp.Body).Decode(&info) == nil && info.Name == config.DisplayName
}

// restartError: start s-a oprit pentru ca update-ul a pus un binar nou; main il porneste.
type restartError struct{ exe string }

func (restartError) Error() string { return "restart after update" }

func start(args []string) error {
	fl := flag.NewFlagSet("start", flag.ExitOnError)
	port := fl.Int("port", 7777, "port the UI listens on")
	noOpen := fl.Bool("no-open", false, "don't open the browser automatically")
	fl.Parse(args)

	paths, err := config.Default()
	if err != nil {
		return err
	}
	// aplicatia se numea foreman: datele din ~/.foreman se muta o data in folderul nou
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	legacyRoot, err := config.MoveLegacyData(home)
	if err != nil {
		return fmt.Errorf("moving data from %s: %w", legacyRoot, err)
	}
	if err := paths.Ensure(); err != nil {
		return fmt.Errorf("cannot create %s: %w", paths.Root, err)
	}
	// Claude Code: daca lipseste, HobStack porneste totusi (pagina de start il instaleaza) si nu porneste run-uri
	claudeBin := claudecli.Find(home)
	if claudeBin == "" {
		log.Printf("claude: not found; HobStack starts without it (install it from the start page)")
	}
	st, err := store.Open(paths.DB)
	if err != nil {
		return err
	}
	defer st.Close()
	if legacyRoot != "" {
		// calea veche din instructiunile agentilor si din brief-uri devine cea noua
		if err := st.RewriteDataRoot(legacyRoot, paths.Root); err != nil {
			return fmt.Errorf("updating paths after moving data: %w", err)
		}
		log.Printf("moved data from %s to %s", legacyRoot, paths.Root)
	}

	// contextul se anuleaza la Ctrl+C / SIGTERM si ajunge in toate request-urile,
	// deci conexiunile SSE se inchid singure la oprire
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// deschidem portul primul, ca sa esuam clar daca e ocupat
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if alreadyRunning(addr) {
			// a doua pornire: deschidem HobStack-ul care ruleaza deja, fara eroare
			log.Printf("%s is already running at http://%s", config.DisplayName, addr)
			if !*noOpen {
				openBrowser("http://" + addr)
			}
			return nil
		}
		return fmt.Errorf("port %d is in use, try --port: %w", *port, err)
	}
	url := "http://" + ln.Addr().String() // portul real, nu cel din flag

	hub := events.NewHub()
	mgr := runner.NewManager(claudeBin, paths.Logs)
	tm := team.New(st, mgr, hub, url)
	if claudeBin == "" {
		tm.Hold(true) // pana il instaleaza utilizatorul (web.SetClaude elibereaza)
	}
	tm.SetFilesRoot(paths.Projects)
	lib := library.New(filepath.Join(paths.Root, "library"), st)
	tm.SetLibrary(lib)
	// dupa un restart: run-urile vechi devin "interrupted", task-urile in lucru revin in coada
	// si echipele pornesc singure (pauza o pune doar utilizatorul, cu Stop team)
	if err := tm.Recover(); err != nil {
		return fmt.Errorf("recovering state: %w", err)
	}
	go tm.Run(ctx)
	// run-urile dinainte de coloana tokens: tokenii se citesc o singura data din loguri
	go backfillTokens(st, paths.Logs)

	// wizard-ul "Proiect nou cu AI": token-urile lui MCP se rezolva inaintea celor ale echipei
	sm := setup.New(mgr, url, filepath.Join(paths.Root, "setup"))
	// "Let AI draft this agent": token-urile lui se rezolva inaintea wizard-ului si a echipei
	dm := draft.New(mgr, url, filepath.Join(paths.Root, "setup"))
	webSrv := web.New(st, tm, mgr, hub, mcp.NewServer(dm.Wrap(sm.Wrap(tm))),
		web.Dirs{Projects: paths.Projects, Workspaces: paths.Workspaces}, picker.Pick, picker.Available())
	webSrv.SetSetup(sm)
	webSrv.SetClaude(ctx, home, claudeBin == "")
	webSrv.SetDraft(dm)
	webSrv.SetLibrary(lib)
	// catalogul local al registrului MCP: cautarea din Add MCP server nu mai depinde de registru.
	// Daca nu se poate deschide, cautarea ramane direct pe registru (nu oprim aplicatia).
	catPath := filepath.Join(paths.Root, "mcp-registry.db")
	if cat, err := mcpregistry.OpenCatalog(catPath, mcpregistry.NewClient().BaseURL); err != nil {
		log.Printf("mcp catalog: cannot open, search goes to the registry: %v", err)
	} else {
		defer cat.Close()
		webSrv.SetCatalog(cat, ctx)
		go syncCatalog(ctx, cat)
	}
	// verificarea zilnica a surselor skill-urilor din Library (la pornire si la fiecare ora)
	go syncSkillSources(ctx, st, skillsrc.NewRemote(), home)
	// butoanele "Deschide": Finder, editor, browser (editoarele se detecteaza acum)
	webSrv.SetOpener(opener.New())

	// update din aplicatie: ultimul release de pe GitHub, restart cand nu mai ruleaza niciun agent
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	var restart atomic.Bool
	if err != nil {
		log.Printf("update: cannot find the running binary: %v", err)
	} else {
		upd := selfupdate.New(exe, selfupdate.Options{Repo: config.UpdateRepo, Version: config.Version}, selfupdate.Deps{
			Active:  func() int { return max(mgr.ActiveCount(), tm.ActiveCount()) },
			Hold:    tm.Hold,
			Backup:  st.Backup,
			Backups: filepath.Join(paths.Root, "backups"),
			Restart: func() { restart.Store(true); stop() },
		})
		webSrv.SetUpdater(upd, ctx)
		go checkUpdates(ctx, upd)
		// Settings > App: pornirea la login si Quit (HobStack ruleaza si fara terminal)
		webSrv.SetApp(exe, home, filepath.Join(paths.Root, config.AppName+".log"), stop)
	}

	srv := &http.Server{
		Handler:           webSrv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	// Windows: iconita langa ceas (clic = deschide, meniul Open / Quit); scoasa la oprire si inainte de restart
	tr := startTray(func() { openBrowser(url) }, stop, func() int { return max(mgr.ActiveCount(), tm.ActiveCount()) })
	defer tr.Close()

	log.Printf("%s %s running at %s", config.AppName, config.Version, url)
	log.Printf("data: %s", paths.Root)
	if claudeBin != "" {
		log.Printf("claude: %s", claudeBin)
	}
	if !*noOpen {
		openBrowser(url)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	log.Println("stopping active agents...")
	mgr.Shutdown()
	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.Shutdown(shCtx)
	if restart.Load() {
		tm.Drain() // un finish in curs isi termina scrierile inainte sa se inchida baza de date
		return restartError{exe: exe}
	}
	return err
}

// checkUpdates verifica la cateva secunde dupa pornire si apoi la fiecare ora daca exista o versiune noua.
func checkUpdates(ctx context.Context, u *selfupdate.Updater) {
	wait := 5 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err := u.Check(ctx); err != nil && ctx.Err() == nil {
			log.Printf("update check: %v", err)
		}
		wait = time.Hour
	}
}

// syncCatalog sincronizeaza catalogul MCP la pornire si apoi la fiecare ora, cand e nevoie (Due).
// Erorile raman in catalog (last_error) si in log; urmatoarea incercare e la urmatorul tick.
func syncCatalog(ctx context.Context, cat *mcpregistry.Catalog) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if cat.Due(time.Now()) {
			err := cat.Sync(ctx)
			// ErrSyncRunning: a pornit "Update now" din UI; context anulat: aplicatia se opreste
			if err != nil && !errors.Is(err, mcpregistry.ErrSyncRunning) && ctx.Err() == nil {
				log.Printf("mcp catalog: sync: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// syncSkillSources verifica sursele skill-urilor la pornire si apoi la fiecare ora;
// CheckSources sare peste cele verificate in ultimele 24 h si se opreste la limita GitHub; home e pentru plugin-uri.
func syncSkillSources(ctx context.Context, st *store.Store, r *skillsrc.Remote, home string) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if err := skillsrc.CheckSources(ctx, st, r, home, time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("skill sources: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// backfillTokens completeaza tokenii run-urilor vechi din logs/runs/<id>.jsonl;
// un run fara log, fara rezultat sau cu 0 tokeni primeste -1, ca sa nu fie citit din nou.
func backfillTokens(st *store.Store, logs string) {
	ids, err := st.RunsWithoutTokens()
	if err != nil {
		log.Printf("tokens backfill: %v", err)
		return
	}
	for _, id := range ids {
		n, ok := runner.LogTokens(filepath.Join(logs, id+".jsonl"))
		if !ok || n == 0 {
			n = -1 // ex. "session limit": rezultat fara tokeni
		}
		if err := st.SetRunTokens(id, n); err != nil {
			log.Printf("tokens backfill %s: %v", id, err)
			return
		}
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("open manually: %s", url)
	}
}
