package main

import (
	"context"
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
	"syscall"
	"time"

	"foreman/internal/config"
	"foreman/internal/draft"
	"foreman/internal/events"
	"foreman/internal/library"
	"foreman/internal/mcp"
	"foreman/internal/opener"
	"foreman/internal/picker"
	"foreman/internal/runner"
	"foreman/internal/setup"
	"foreman/internal/store"
	"foreman/internal/team"
	"foreman/internal/web"
)

const usage = `%[1]s - local Claude Code agent teams

Usage:
  %[1]s start [--port 7777] [--no-open]
`

func main() {
	if len(os.Args) < 2 {
		fmt.Printf(usage, config.AppName)
		os.Exit(1)
	}
	var err error
	switch os.Args[1] {
	case "start":
		err = start(os.Args[2:])
	default:
		fmt.Printf(usage, config.AppName)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func start(args []string) error {
	fl := flag.NewFlagSet("start", flag.ExitOnError)
	port := fl.Int("port", 7777, "port the UI listens on")
	noOpen := fl.Bool("no-open", false, "don't open the browser automatically")
	fl.Parse(args)

	paths, err := config.Default()
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return fmt.Errorf("cannot create %s: %w", paths.Root, err)
	}
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return errors.New("'claude' not found in PATH. Install Claude Code and try again")
	}
	st, err := store.Open(paths.DB)
	if err != nil {
		return err
	}
	defer st.Close()

	// contextul se anuleaza la Ctrl+C / SIGTERM si ajunge in toate request-urile,
	// deci conexiunile SSE se inchid singure la oprire
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// deschidem portul primul, ca sa esuam clar daca e ocupat
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is in use, try --port: %w", *port, err)
	}
	url := "http://" + ln.Addr().String() // portul real, nu cel din flag

	hub := events.NewHub()
	mgr := runner.NewManager(claudeBin, paths.Logs)
	tm := team.New(st, mgr, hub, url)
	tm.SetFilesRoot(paths.Projects)
	tm.SetLibrary(library.New(filepath.Join(paths.Root, "library"), st))
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
	webSrv.SetDraft(dm)
	// butoanele "Deschide": Finder, editor, browser (editoarele se detecteaza acum)
	webSrv.SetOpener(opener.New())

	srv := &http.Server{
		Handler:           webSrv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	log.Printf("%s running at %s", config.AppName, url)
	log.Printf("data: %s", paths.Root)
	log.Printf("claude: %s", claudeBin)
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
	return srv.Shutdown(shCtx)
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
