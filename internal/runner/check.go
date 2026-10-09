package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Check trimite un mesaj scurt cu contul dat (env) si spune daca autentificarea merge.
// Ruleaza intr-un folder temporar gol, fara tool-uri (o singura tura), direct, nu prin
// Start: fara log si fara SSE. Fara setarile utilizatorului, serverele MCP si skill-urile:
// cu ele, pluginurile instalate umflau contextul la ~130k tokeni ($0.13 pe verificare,
// masurat cu claude 2.1.295); asa costa sub o zecime de cent.
func (m *Manager) Check(ctx context.Context, env []string) error {
	dir, err := os.MkdirTemp("", "hobstack-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, m.claudeBin, "-p", "Reply with OK", "--model", "haiku",
		"--output-format", "json", "--setting-sources", "", "--strict-mcp-config",
		"--disable-slash-commands", "--tools", "")
	cmd.Dir = dir
	cmd.Env = procEnv(env)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	if res, ok := lastResult(out); ok {
		if !res.IsError {
			return nil
		}
		for _, msg := range []string{strings.TrimSpace(res.Result), strings.Join(res.Errors, "; "), res.Subtype} {
			if msg != "" {
				return errors.New(msg)
			}
		}
		return errors.New("the check failed")
	}
	if ctx.Err() != nil {
		return errors.New("claude did not answer in time")
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return errors.New(msg)
	}
	if runErr != nil {
		return fmt.Errorf("cannot run claude: %w", runErr)
	}
	return errors.New("claude exited without a result")
}

type checkResult struct {
	Type    string   `json:"type"`
	Subtype string   `json:"subtype"`
	IsError bool     `json:"is_error"`
	Result  string   `json:"result"`
	Errors  []string `json:"errors"`
}

// lastResult cauta ultimul eveniment "result" din output (json sau stream-json).
func lastResult(out []byte) (checkResult, bool) {
	var found checkResult
	ok := false
	for _, line := range bytes.Split(out, []byte("\n")) {
		var r checkResult
		if json.Unmarshal(bytes.TrimSpace(line), &r) == nil && r.Type == "result" {
			found, ok = r, true
		}
	}
	return found, ok
}

// AuthStatus: contul implicit, din `claude auth status --json` (mediul mostenit, ca la Default).
type AuthStatus struct {
	LoggedIn         bool   `json:"loggedIn"`
	AuthMethod       string `json:"authMethod"`
	Email            string `json:"email"`
	SubscriptionType string `json:"subscriptionType"`
}

func (m *Manager) AuthStatus(ctx context.Context) (AuthStatus, error) {
	out, err := exec.CommandContext(ctx, m.claudeBin, "auth", "status", "--json").Output()
	var st AuthStatus
	// delogat: claude poate iesi cu cod nenul, dar tot scrie JSON-ul
	if jerr := json.Unmarshal(out, &st); jerr != nil {
		if err != nil {
			return AuthStatus{}, err
		}
		return AuthStatus{}, jerr
	}
	return st, nil
}
