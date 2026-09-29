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
	"runtime"
	"syscall"
	"time"

	"foreman/internal/config"
	"foreman/internal/events"
	"foreman/internal/mcp"
	"foreman/internal/picker"
	"foreman/internal/runner"
	"foreman/internal/store"
	"foreman/internal/team"
	"foreman/internal/web"
)

const usage = `%[1]s - echipe de agenti Claude Code, local

Utilizare:
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
		fmt.Fprintln(os.Stderr, "eroare:", err)
		os.Exit(1)
	}
}

func start(args []string) error {
	fl := flag.NewFlagSet("start", flag.ExitOnError)
	port := fl.Int("port", 7777, "portul pe care asculta UI-ul")
	noOpen := fl.Bool("no-open", false, "nu deschide browserul automat")
	fl.Parse(args)

	paths, err := config.Default()
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return fmt.Errorf("nu pot crea %s: %w", paths.Root, err)
	}
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return errors.New("nu gasesc 'claude' in PATH. Instaleaza Claude Code si incearca din nou")
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
		return fmt.Errorf("portul %d e ocupat, incearca --port: %w", *port, err)
	}
	url := "http://" + ln.Addr().String() // portul real, nu cel din flag

	hub := events.NewHub()
	mgr := runner.NewManager(claudeBin, paths.Logs)
	tm := team.New(st, mgr, hub, url)
	tm.SetFilesRoot(paths.Projects)
	// dupa un restart: run-urile vechi devin "interrupted" si totul sta pe pauza
	if err := tm.Recover(); err != nil {
		return fmt.Errorf("recuperarea starii: %w", err)
	}
	go tm.Run(ctx)

	srv := &http.Server{
		Handler: web.New(st, tm, mgr, hub, mcp.NewServer(tm),
			web.Dirs{Projects: paths.Projects, Workspaces: paths.Workspaces}, picker.Pick, picker.Available()).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	log.Printf("%s ruleaza pe %s", config.AppName, url)
	log.Printf("date: %s", paths.Root)
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

	log.Println("opresc agentii activi...")
	mgr.Shutdown()
	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shCtx)
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
		log.Printf("deschide manual: %s", url)
	}
}
