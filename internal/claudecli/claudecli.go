// Package claudecli gaseste Claude Code (claude) pe masina, il instaleaza cu installerul oficial la cererea
// utilizatorului, pune folderul lui in PATH (ca sa mearga si in terminal) si deschide un terminal pentru login.
// HobStack ruleaza claude prin calea absoluta, deci PATH-ul conteaza doar pentru terminal.
package claudecli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Comenzile installerului oficial (https://code.claude.com/docs/en/setup).
var installCmd = map[string][]string{
	"windows": {"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "irm https://claude.ai/install.ps1 | iex"},
	"unix":    {"bash", "-c", "curl -fsSL https://claude.ai/install.sh | bash"},
}

// InstallCommand intoarce comanda installerului pentru sistemul curent (afisata in UI inainte de Install).
func InstallCommand() []string {
	if runtime.GOOS == "windows" {
		return installCmd["windows"]
	}
	return installCmd["unix"]
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return "claude.exe"
	}
	return "claude"
}

// LocalBin: unde pune installerul nativ claude (~/.local/bin).
func LocalBin(home string) string { return filepath.Join(home, ".local", "bin") }

// Find cauta claude: PATH, apoi locurile obisnuite. Pornit cu dublu-clic (Explorer / Finder), PATH-ul
// procesului e altul decat in terminal, deci nu ne bazam doar pe el. "" = nu e instalat.
func Find(home string) string {
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	candidates := []string{
		filepath.Join(LocalBin(home), exeName()),
		filepath.Join(home, ".claude", "local", exeName()), // instalarea locala veche
	}
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			candidates = append(candidates, filepath.Join(appdata, "npm", "claude.cmd")) // npm install -g
		}
	} else {
		candidates = append(candidates, "/opt/homebrew/bin/claude", "/usr/local/bin/claude", filepath.Join(home, ".npm-global", "bin", "claude"))
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return ""
}

// Version ruleaza `claude --version` (de ex. "2.1.318 (Claude Code)").
func Version(ctx context.Context, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// MaxLog: cate linii din output-ul installerului pastram.
const MaxLog = 300

// Installer ruleaza installerul oficial o singura data odata si tine output-ul pentru UI.
type Installer struct {
	mu      sync.Mutex
	running bool
	log     []string
	err     string
	done    bool
	cmd     func(ctx context.Context) *exec.Cmd // testele dau alta comanda
}

func NewInstaller() *Installer {
	return &Installer{cmd: func(ctx context.Context) *exec.Cmd {
		c := InstallCommand()
		return exec.CommandContext(ctx, c[0], c[1:]...)
	}}
}

// InstallState e ce vede UI-ul.
type InstallState struct {
	Running bool     `json:"running"`
	Done    bool     `json:"done"` // a rulat cel putin o data (cu sau fara eroare)
	Error   string   `json:"error,omitempty"`
	Log     []string `json:"log"`
}

func (in *Installer) State() InstallState {
	in.mu.Lock()
	defer in.mu.Unlock()
	return InstallState{Running: in.running, Done: in.done, Error: in.err, Log: append([]string{}, in.log...)}
}

// ErrRunning: installerul ruleaza deja.
var ErrRunning = errors.New("the installer is already running")

// Start porneste installerul in fundal; onDone e chemat la final (si la eroare).
func (in *Installer) Start(ctx context.Context, onDone func(error)) error {
	in.mu.Lock()
	if in.running {
		in.mu.Unlock()
		return ErrRunning
	}
	in.running, in.done, in.err, in.log = true, false, "", nil
	in.mu.Unlock()
	go func() {
		err := in.run(ctx)
		in.mu.Lock()
		in.running, in.done = false, true
		if err != nil {
			in.err = err.Error()
		}
		in.mu.Unlock()
		if onDone != nil {
			onDone(err)
		}
	}()
	return nil
}

func (in *Installer) add(line string) {
	line = strings.TrimRight(line, "\r")
	in.mu.Lock()
	in.log = append(in.log, line)
	if len(in.log) > MaxLog {
		in.log = in.log[len(in.log)-MaxLog:]
	}
	in.mu.Unlock()
}

func (in *Installer) run(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := in.cmd(ctx)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		return fmt.Errorf("cannot start the installer: %w", err)
	}
	read := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			in.add(stripANSI(sc.Text()))
		}
		io.Copy(io.Discard, pr)
		close(read)
	}()
	err := cmd.Wait()
	pw.Close()
	<-read
	if err != nil {
		return fmt.Errorf("the installer failed: %w", err)
	}
	return nil
}

// stripANSI scoate culorile terminalului din output-ul installerului.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
