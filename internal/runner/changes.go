package runner

import (
	"encoding/json"
	"strings"
)

const (
	maxPatchChars = 200000 // suma lungimilor liniilor dintr-un patch
	maxCommandLen = 2000
)

// Hunk e un bloc din structuredPatch-ul lui claude.
// Lines au prefix "-", "+" sau " " (context).
type Hunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

// FileChange e o modificare de fisier facuta de un tool (Write, Edit, ...).
type FileChange struct {
	Tool      string // "Write", "Edit", "MultiEdit", "NotebookEdit"
	Path      string // absolut, ca in input
	Kind      string // "create", "update" (Write peste existent), "edit"
	Patch     []Hunk
	Truncated bool
}

// pendingCall e un tool_use vazut, in asteptarea rezultatului.
type pendingCall struct {
	name  string
	input map[string]any
}

func (t *tracker) observeAssistant(line []byte) {
	var ev struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	var blocks []struct {
		Type  string         `json:"type"`
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	}
	if json.Unmarshal(ev.Message.Content, &blocks) != nil {
		return // content poate fi si string
	}
	for _, b := range blocks {
		if b.Type != "tool_use" || b.ID == "" {
			continue
		}
		t.toolUses = append(t.toolUses, b.Name)
		switch b.Name {
		case "Write", "Edit", "MultiEdit", "NotebookEdit", "Bash":
			if t.pending == nil {
				t.pending = map[string]pendingCall{}
			}
			t.pending[b.ID] = pendingCall{name: b.Name, input: b.Input}
		}
	}
}

func (t *tracker) observeUser(line []byte) {
	var ev struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		ToolUseResult json.RawMessage `json:"tool_use_result"`
	}
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	var blocks []struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
		IsError   *bool  `json:"is_error"` // poate fi null
	}
	if json.Unmarshal(ev.Message.Content, &blocks) != nil {
		return // content poate fi string
	}
	var results int
	for _, b := range blocks {
		if b.Type == "tool_result" {
			results++
		}
	}
	for _, b := range blocks {
		if b.Type != "tool_result" {
			continue
		}
		call, ok := t.pending[b.ToolUseID]
		if !ok {
			continue
		}
		delete(t.pending, b.ToolUseID)
		failed := b.IsError != nil && *b.IsError
		if call.name == "Bash" {
			// si comenzile esuate conteaza
			if cmd, _ := call.input["command"].(string); cmd != "" {
				if r := []rune(cmd); len(r) > maxCommandLen {
					cmd = string(r[:maxCommandLen]) + "..."
				}
				t.commands = append(t.commands, Command{Text: cmd, OK: !failed})
			}
			continue
		}
		if failed {
			continue
		}
		// tool_use_result e unul per eveniment: il folosim doar cand e un singur rezultat
		var raw json.RawMessage
		if results == 1 {
			raw = ev.ToolUseResult
		}
		t.changes = append(t.changes, buildChange(call, raw))
	}
}

// buildChange face FileChange din apel si, daca exista, tool_use_result.
func buildChange(call pendingCall, raw json.RawMessage) FileChange {
	in := call.input
	str := func(k string) string { s, _ := in[k].(string); return s }
	fc := FileChange{Tool: call.name, Path: str("file_path"), Kind: "edit"}
	if call.name == "NotebookEdit" {
		fc.Path = str("notebook_path")
	}

	var res struct {
		Type            string `json:"type"`
		Content         string `json:"content"`
		StructuredPatch []Hunk `json:"structuredPatch"`
	}
	hasRes := len(raw) > 0 && json.Unmarshal(raw, &res) == nil

	if call.name == "Write" {
		fc.Kind = "create"
		if hasRes && res.Type == "update" {
			fc.Kind = "update"
		}
		if hasRes && res.Type == "update" && res.StructuredPatch != nil {
			fc.Patch = res.StructuredPatch
		} else {
			content := str("content")
			if hasRes && res.Type == "create" {
				content = res.Content
			}
			fc.Patch = createHunk(content)
		}
		fc.Patch, fc.Truncated = truncatePatch(fc.Patch)
		return fc
	}

	if hasRes && res.StructuredPatch != nil {
		fc.Patch = res.StructuredPatch
	} else {
		fc.Patch = fallbackPatch(call)
	}
	fc.Patch, fc.Truncated = truncatePatch(fc.Patch)
	return fc
}

// splitLines desparte textul in linii, fara elementul gol de dupa ultimul \n.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func prefixed(prefix, s string) []string {
	lines := splitLines(s)
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return lines
}

// createHunk: un singur bloc {0,0,1,N} cu toate liniile "+".
func createHunk(content string) []Hunk {
	lines := prefixed("+", content)
	if len(lines) == 0 {
		return nil
	}
	return []Hunk{{OldStart: 0, OldLines: 0, NewStart: 1, NewLines: len(lines), Lines: lines}}
}

// fallbackPatch construieste patch-ul din input cand lipseste tool_use_result:
// blocuri fara numere de linie, "-" pentru old_string si "+" pentru new_string.
func fallbackPatch(call pendingCall) []Hunk {
	in := call.input
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }
	block := func(oldS, newS string) Hunk {
		old, nw := prefixed("-", oldS), prefixed("+", newS)
		return Hunk{OldLines: len(old), NewLines: len(nw), Lines: append(old, nw...)}
	}
	switch call.name {
	case "Edit":
		return []Hunk{block(str(in, "old_string"), str(in, "new_string"))}
	case "MultiEdit":
		var out []Hunk
		edits, _ := in["edits"].([]any)
		for _, e := range edits {
			if m, ok := e.(map[string]any); ok {
				out = append(out, block(str(m, "old_string"), str(m, "new_string")))
			}
		}
		return out
	case "NotebookEdit":
		return []Hunk{block("", str(in, "new_source"))}
	}
	return nil
}

// truncatePatch taie patch-ul la maxPatchChars (suma lungimilor liniilor):
// scoate blocurile de la final si scurteaza ultimul bloc pastrat.
func truncatePatch(p []Hunk) ([]Hunk, bool) {
	total := 0
	for _, h := range p {
		for _, l := range h.Lines {
			total += len([]rune(l))
		}
	}
	if total <= maxPatchChars {
		return p, false
	}
	var out []Hunk
	left := maxPatchChars
	for _, h := range p {
		if left <= 0 {
			break
		}
		kept := Hunk{OldStart: h.OldStart, OldLines: h.OldLines, NewStart: h.NewStart, NewLines: h.NewLines}
		for _, l := range h.Lines {
			r := []rune(l)
			if len(r) > left {
				if left > 0 {
					kept.Lines = append(kept.Lines, string(r[:left]))
				}
				left = 0
				break
			}
			kept.Lines = append(kept.Lines, l)
			left -= len(r)
		}
		out = append(out, kept)
	}
	return out, true
}
