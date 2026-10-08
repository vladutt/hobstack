package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func denialsOf(line string) []Denial {
	var tr tracker
	tr.observe([]byte(line))
	return tr.outcome("r", nil, false).Denials
}

func TestTrackerDenials(t *testing.T) {
	d := denialsOf(`{"type":"result","permission_denials":[` +
		`{"tool_name":"Bash","tool_input":{"command":"ls /","description":"x"}},` +
		`{"tool_name":"Edit","tool_input":{"file_path":"/a/b.go","old_string":"x"}},` +
		`{"tool_name":"Foo","tool_input":{"a":1}}]}`)
	if len(d) != 3 || d[0] != (Denial{"Bash", "ls /"}) || d[1] != (Denial{"Edit", "/a/b.go"}) || d[2] != (Denial{"Foo", `{"a":1}`}) {
		t.Fatalf("%+v", d)
	}
	if d := denialsOf(`{"type":"result","result":"ok"}`); d != nil {
		t.Fatalf("%+v", d)
	}
	long := strings.Repeat("x", 200)
	d = denialsOf(`{"type":"result","is_error":true,"permission_denials":[{"tool_name":"Bash","tool_input":{"command":"` + long + `"}}]}`)
	if len(d) != 1 || d[0].Detail != long[:120]+"..." {
		t.Fatalf("%+v", d)
	}
}

func TestTrackerTokens(t *testing.T) {
	var tr tracker
	tr.observe([]byte(`{"type":"result","total_cost_usd":0.5,"usage":{"input_tokens":8,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000,"output_tokens":20}}`))
	if o := tr.outcome("r", nil, false); o.Tokens != 1128 || o.CostUSD != 0.5 {
		t.Fatalf("%+v", o)
	}
}

func TestLogTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.jsonl")
	os.WriteFile(path, []byte(`{"type":"system"}`+"\n"+`{"type":"result","usage":{"input_tokens":1,"output_tokens":2}}`+"\n"), 0o644)
	if n, ok := LogTokens(path); !ok || n != 3 {
		t.Fatalf("%d %v", n, ok)
	}
	os.WriteFile(path, []byte(`{"type":"assistant"}`+"\n"), 0o644)
	if _, ok := LogTokens(path); ok {
		t.Fatal("log fara rezultat")
	}
	if _, ok := LogTokens(filepath.Join(dir, "lipsa.jsonl")); ok {
		t.Fatal("log lipsa")
	}
}
