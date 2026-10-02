package runner

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func use(id, name, input string) string {
	return `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":` + input + `}]}}`
}

func res(id, isErr, tur string) string {
	s := `{"type":"user","message":{"role":"user","content":[{"tool_use_id":"` + id + `","type":"tool_result","content":"x"` + isErr + `}]}`
	if tur != "" {
		s += `,"tool_use_result":` + tur
	}
	return s + `}`
}

func feed(lines ...string) Outcome {
	var tr tracker
	for _, l := range lines {
		tr.observe([]byte(l))
	}
	return tr.outcome("r", nil, false)
}

func TestChangesWriteCreate(t *testing.T) {
	o := feed(
		use("w1", "Write", `{"file_path":"/p/a.txt","content":"hello\nworld\n"}`),
		res("w1", "", `{"type":"create","filePath":"/p/a.txt","content":"hello\nworld\n","structuredPatch":[],"originalFile":null}`),
	)
	want := []FileChange{{Tool: "Write", Path: "/p/a.txt", Kind: "create",
		Patch: []Hunk{{0, 0, 1, 2, []string{"+hello", "+world"}}}}}
	if !reflect.DeepEqual(o.Changes, want) {
		t.Fatalf("%+v", o.Changes)
	}
}

func TestChangesWriteUpdate(t *testing.T) {
	o := feed(
		use("w1", "Write", `{"file_path":"/p/a.txt","content":"new"}`),
		res("w1", `,"is_error":null`, `{"type":"update","filePath":"/p/a.txt","content":"new","structuredPatch":[{"oldStart":1,"oldLines":1,"newStart":1,"newLines":1,"lines":["-old","+new"]}],"originalFile":"old"}`),
	)
	want := []FileChange{{Tool: "Write", Path: "/p/a.txt", Kind: "update",
		Patch: []Hunk{{1, 1, 1, 1, []string{"-old", "+new"}}}}}
	if !reflect.DeepEqual(o.Changes, want) {
		t.Fatalf("%+v", o.Changes)
	}
}

func TestChangesEdit(t *testing.T) {
	o := feed(
		use("e1", "Edit", `{"replace_all":false,"file_path":"/p/a.txt","old_string":"hello","new_string":"hello world"}`),
		res("e1", "", `{"filePath":"/p/a.txt","oldString":"hello","newString":"hello world","originalFile":"hello","structuredPatch":[{"oldStart":1,"oldLines":1,"newStart":1,"newLines":1,"lines":["-hello","\\ No newline at end of file","+hello world","\\ No newline at end of file"]}]}`),
	)
	if len(o.Changes) != 1 || o.Changes[0].Kind != "edit" || o.Changes[0].Tool != "Edit" ||
		len(o.Changes[0].Patch) != 1 || len(o.Changes[0].Patch[0].Lines) != 4 {
		t.Fatalf("%+v", o.Changes)
	}
}

func TestChangesMultiEdit(t *testing.T) {
	o := feed(
		use("m1", "MultiEdit", `{"file_path":"/p/a.go","edits":[{"old_string":"a","new_string":"b"},{"old_string":"c","new_string":"d"}]}`),
		res("m1", `,"is_error":false`, `{"filePath":"/p/a.go","structuredPatch":[{"oldStart":1,"oldLines":1,"newStart":1,"newLines":1,"lines":["-a","+b"]},{"oldStart":9,"oldLines":1,"newStart":9,"newLines":1,"lines":["-c","+d"]}]}`),
	)
	if len(o.Changes) != 1 || o.Changes[0].Tool != "MultiEdit" || len(o.Changes[0].Patch) != 2 {
		t.Fatalf("%+v", o.Changes)
	}
}

func TestChangesFailedAndUnansweredIgnored(t *testing.T) {
	o := feed(
		use("e1", "Edit", `{"file_path":"/p/a","old_string":"x","new_string":"y"}`),
		res("e1", `,"is_error":true`, ""),
		use("e2", "Edit", `{"file_path":"/p/b","old_string":"x","new_string":"y"}`),
		// user cu content string: fara erori
		`{"type":"user","message":{"role":"user","content":"salut"}}`,
	)
	if len(o.Changes) != 0 {
		t.Fatalf("%+v", o.Changes)
	}
}

func TestChangesFallback(t *testing.T) {
	o := feed(
		use("e1", "Edit", `{"file_path":"/p/a","old_string":"a\nb","new_string":"c"}`),
		res("e1", "", ""),
		use("w1", "Write", `{"file_path":"/p/b","content":"x\ny"}`),
		res("w1", "", ""),
		use("n1", "NotebookEdit", `{"notebook_path":"/p/n.ipynb","new_source":"print(1)"}`),
		res("n1", "", `{"something":"else"}`),
	)
	if len(o.Changes) != 3 {
		t.Fatalf("%+v", o.Changes)
	}
	if h := o.Changes[0].Patch; !reflect.DeepEqual(h, []Hunk{{0, 2, 0, 1, []string{"-a", "-b", "+c"}}}) {
		t.Fatalf("%+v", h)
	}
	if c := o.Changes[1]; c.Kind != "create" || !reflect.DeepEqual(c.Patch, []Hunk{{0, 0, 1, 2, []string{"+x", "+y"}}}) {
		t.Fatalf("%+v", c)
	}
	if c := o.Changes[2]; c.Path != "/p/n.ipynb" || c.Kind != "edit" || c.Patch[0].Lines[0] != "+print(1)" {
		t.Fatalf("%+v", c)
	}
}

func TestChangesTwoResultsInOneEventUseFallback(t *testing.T) {
	ev := `{"type":"user","message":{"role":"user","content":[` +
		`{"tool_use_id":"a","type":"tool_result"},{"tool_use_id":"b","type":"tool_result"}]},` +
		`"tool_use_result":{"structuredPatch":[{"oldStart":5,"oldLines":1,"newStart":5,"newLines":1,"lines":["-zz","+yy"]}]}}`
	o := feed(
		use("a", "Edit", `{"file_path":"/p/a","old_string":"x","new_string":"y"}`),
		use("b", "Edit", `{"file_path":"/p/b","old_string":"x","new_string":"y"}`),
		ev,
	)
	if len(o.Changes) != 2 || o.Changes[0].Patch[0].OldStart != 0 || o.Changes[1].Patch[0].Lines[0] != "-x" {
		t.Fatalf("%+v", o.Changes)
	}
}

func TestCommands(t *testing.T) {
	long := strings.Repeat("x", 2500)
	cmd, _ := json.Marshal(long)
	o := feed(
		use("b1", "Bash", `{"command":"ls","description":"d"}`),
		res("b1", `,"is_error":false`, `{"stdout":"","stderr":""}`),
		use("b2", "Bash", `{"command":"false"}`),
		res("b2", `,"is_error":true`, ""),
		use("b3", "Bash", `{"command":`+string(cmd)+`}`),
		res("b3", "", ""),
	)
	if len(o.Commands) != 3 || o.Commands[0] != (Command{"ls", true}) || o.Commands[1] != (Command{"false", false}) ||
		o.Commands[2] != (Command{long[:2000] + "...", true}) || len(o.Changes) != 0 {
		t.Fatalf("%+v", o.Commands)
	}
}

func TestPatchTruncation(t *testing.T) {
	big := strings.Repeat("a", 150000)
	o := feed(
		use("e1", "Edit", `{"file_path":"/p/a","old_string":"x","new_string":"y"}`),
		res("e1", "", `{"structuredPatch":[`+
			`{"oldStart":1,"oldLines":1,"newStart":1,"newLines":1,"lines":["+`+big+`"]},`+
			`{"oldStart":5,"oldLines":1,"newStart":5,"newLines":1,"lines":["+`+big+`","+b"]},`+
			`{"oldStart":9,"oldLines":1,"newStart":9,"newLines":1,"lines":["+c"]}]}`),
	)
	c := o.Changes[0]
	if !c.Truncated || len(c.Patch) != 2 {
		t.Fatalf("truncated=%v hunks=%d", c.Truncated, len(c.Patch))
	}
	n := 0
	for _, h := range c.Patch {
		for _, l := range h.Lines {
			n += len(l)
		}
	}
	if n != maxPatchChars || len(c.Patch[1].Lines) != 1 {
		t.Fatalf("n=%d", n)
	}
	// fara taiere: flag fals
	o = feed(use("e1", "Edit", `{"file_path":"/p/a","old_string":"x","new_string":"y"}`), res("e1", "", ""))
	if o.Changes[0].Truncated {
		t.Fatal("nu trebuia taiat")
	}
}
