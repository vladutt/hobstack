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

	"agentdash/internal/agent"
	"agentdash/internal/config"
	"agentdash/internal/runner"
	"agentdash/internal/web"
)

const usage = `agentdash - control local pentru agentii Claude Code

Utilizare:
  agentdash start [--port 7777] [--no-open]
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "start":
		err = start(os.Args[2:])
	default:
		fmt.Print(usage)
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
	if err := agent.EnsureDemo(paths.Agents); err != nil {
		return err
	}

	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return errors.New("nu gasesc 'claude' in PATH. Instaleaza Claude Code si incearca din nou")
	}

	// contextul se anuleaza la Ctrl+C / SIGTERM si ajunge in toate request-urile,
	// deci conexiunile SSE se inchid singure la oprire
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mgr := runner.NewManager(claudeBin, paths.Logs)
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           web.New(paths, mgr).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	// deschidem portul inainte de browser, ca sa esuam clar daca e ocupat
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("portul %d e ocupat, incearca --port: %w", *port, err)
	}

	url := "http://" + addr
	log.Printf("agentdash ruleaza pe %s", url)
	log.Printf("agenti: %s", paths.Agents)
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
