package prompt

import (
	"fmt"
	"strings"

	"hobstack/internal/store"
)

// Limitele indexului din tura (spec: memoria proiectului).
const (
	memoryIndexLines = 40
	memoryIndexChars = 3000
	memoryMoreRoom   = 60 // loc pastrat pentru randul "... N more"
)

// howMemory: regula pentru toti agentii; indexul vine in tura, nu aici (system prompt-ul nu se schimba cu memoria).
const howMemory = `Project memory holds decisions and facts that every agent of this project must follow; it is listed in your turn. ` +
	`Follow it. Save to it with mcp__team__remember only what outlives this task: a general rule behind a send-back or a ` +
	`rejection, a decision the user made (in chat, when unblocking, in Decide), an architecture or convention choice. Use a ` +
	`short stable key (area.topic) and update the existing key instead of adding a near duplicate. Do not save task ` +
	`progress, file lists or anything already in the project's shared context. If an entry is wrong or outdated, update or ` +
	`forget it and say why.`

// MemoryIndex: blocul "Project memory" din tura, in ordinea primita (store.ListMemory: blocate, apoi recente).
// withTools = false pentru run-urile fara tool-urile de memorie (brainstorming). Gol = "".
func MemoryIndex(entries []store.Memory, withTools bool) string {
	if len(entries) == 0 {
		return ""
	}
	head := "Project memory (decisions and facts saved by the team):"
	if withTools {
		head = "Project memory (decisions and facts saved by the team; read the full text with mcp__team__recall):"
	}
	var b strings.Builder
	b.WriteString(head)
	size := len([]rune(head))
	shown := 0
	for _, m := range entries {
		if shown == memoryIndexLines {
			break
		}
		meta := m.AuthorName
		if m.Locked {
			meta = "locked, " + meta
		}
		if m.TaskRef != "" {
			meta += ", " + m.TaskRef
		}
		line := fmt.Sprintf("\n- %s: %s (%s)", m.Key, m.Summary, meta)
		n := len([]rune(line))
		if size+n > memoryIndexChars-memoryMoreRoom {
			break
		}
		b.WriteString(line)
		size += n
		shown++
	}
	if rest := len(entries) - shown; rest > 0 {
		fmt.Fprintf(&b, "\n- ... %d more", rest)
		if withTools {
			b.WriteString(": search with mcp__team__search_memory")
		}
	}
	return b.String()
}
