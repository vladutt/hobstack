package library

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// ProbeMCP porneste un server MCP local, ii cere tools/list si intoarce numele tool-urilor.
// Procesul e oprit la timeout si la intoarcere.
func ProbeMCP(command string, args []string, env map[string]string, timeout time.Duration) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...) // la ctx.Done() omoara procesul
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.WaitDelay = time.Second // nu asteptam la nesfarsit dupa pipe-uri tinute de copii
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		cancel()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"foreman","version":"1"}}}`
	for _, line := range []string{init,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`} {
		if _, err := io.WriteString(stdin, line+"\n"); err != nil {
			return nil, fmt.Errorf("server closed its input: %w", err)
		}
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20) // o linie are cel mult 1 MB
	for sc.Scan() {
		var m struct {
			ID    json.Number `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.ID.String() != "2" {
			continue // log sau alt raspuns
		}
		if m.Error != nil {
			return nil, errors.New("tools/list failed: " + m.Error.Message)
		}
		names := []string{}
		for _, t := range m.Result.Tools {
			names = append(names, t.Name)
		}
		return names, nil
	}
	if ctx.Err() != nil {
		return nil, errors.New("the server did not answer in time")
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading the server output: %w", err)
	}
	return nil, errors.New("the server exited without listing its tools")
}
