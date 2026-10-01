package runner

import (
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
