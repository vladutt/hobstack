package library

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestHelperMCP e un server MCP minimal: testul se re-porneste ca proces.
func TestHelperMCP(t *testing.T) {
	if os.Getenv("FOREMAN_HELPER_MCP") != "1" {
		return
	}
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var m struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		json.Unmarshal(sc.Bytes(), &m)
		if m.ID == nil {
			continue
		}
		var r any = map[string]any{}
		switch m.Method {
		case "initialize":
			r = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "t", "version": "1"}}
		case "tools/list":
			tools := []any{map[string]any{"name": "list_vehicles"}, map[string]any{"name": "get_vehicle"}}
			if os.Getenv("FOREMAN_TEST_SECRET") != "" { // mediul Foreman nu trebuie sa ajunga la server
				tools = append(tools, map[string]any{"name": "leaked_secret"})
			}
			if os.Getenv("FOREMAN_HELPER_EXTRA") != "" { // mediul dat explicit ajunge
				tools = append(tools, map[string]any{"name": "extra_env"})
			}
			r = map[string]any{"tools": tools}
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": r})
		fmt.Println(string(out))
	}
	os.Exit(0)
}

func TestProbeMCP(t *testing.T) {
	names, err := ProbeMCP(os.Args[0], []string{"-test.run=TestHelperMCP"}, map[string]string{"FOREMAN_HELPER_MCP": "1"}, 5*time.Second)
	if err != nil || strings.Join(names, ",") != "list_vehicles,get_vehicle" {
		t.Fatalf("%v %v", names, err)
	}
	start := time.Now()
	if _, err := ProbeMCP("sleep", []string{"30"}, nil, 300*time.Millisecond); err == nil {
		t.Fatal("un server care nu raspunde trebuia sa expire")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("procesul nu a fost oprit la timp")
	}
}

func TestProbeMCPScrubbedEnv(t *testing.T) {
	t.Setenv("FOREMAN_TEST_SECRET", "s3cret")
	env := map[string]string{"FOREMAN_HELPER_MCP": "1", "FOREMAN_HELPER_EXTRA": "1"}
	names, err := ProbeMCP(os.Args[0], []string{"-test.run=TestHelperMCP"}, env, 5*time.Second)
	if err != nil || strings.Join(names, ",") != "list_vehicles,get_vehicle,extra_env" {
		t.Fatalf("%v %v", names, err)
	}
}

func TestProbeMCPHugeLine(t *testing.T) {
	// o linie de peste 1 MB trebuie sa dea eroare, nu sa umple memoria
	if _, err := ProbeMCP("sh", []string{"-c", "head -c 3000000 /dev/zero | tr '\\0' a; sleep 5"}, nil, 3*time.Second); err == nil {
		t.Fatal("linie uriasa acceptata")
	}
}

// gone verifica (cu pgrep) ca nu mai exista procese cu argumentul-marcaj.
func gone(t *testing.T, marker string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		if exec.Command("pgrep", "-f", marker).Run() != nil {
			return // pgrep intoarce eroare cand nu gaseste nimic
		}
		time.Sleep(100 * time.Millisecond)
	}
	exec.Command("pkill", "-f", marker).Run()
	t.Fatalf("procese ramase: %s", marker)
}

func TestProbeMCPKillsProcessTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fara process group")
	}
	for _, c := range []struct{ script, marker string }{
		{"sleep 31.123 & sleep 32.123", "sleep 31.123"},
		{"sh -c 'cat >/dev/null; sleep 33.123'; true", "sleep 33.123"},
	} {
		start := time.Now()
		if _, err := ProbeMCP("sh", []string{"-c", c.script}, nil, 300*time.Millisecond); err == nil {
			t.Fatal("trebuia sa expire:", c.script)
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Fatalf("%q a durat %v", c.script, d)
		}
		gone(t, c.marker)
	}
}
