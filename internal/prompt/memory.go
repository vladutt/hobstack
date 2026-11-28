package prompt

import (
	"fmt"
	"strings"

	"hobstack/internal/store"
)

// Limitele indexului din tura (spec: memoria proiectului).
const (
	memoryIndexMax   = 40
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
	head, lines := memoryIndexLines(entries, withTools)
	var b strings.Builder
	b.WriteString(head)
	for _, l := range lines {
		b.WriteString(l)
	}
	if rest := len(entries) - len(lines); rest > 0 {
		fmt.Fprintf(&b, "\n- ... %d more", rest)
		if withTools {
			b.WriteString(": search with mcp__team__search_memory")
		}
	}
	return b.String()
}

// MemoryIndexShown: cate intrari (primele, in ordinea primita) intra in indexul din tura unui run cu tool-uri.
// Tab-ul Memory le marcheaza pe celelalte (agentii le gasesc doar cautand).
func MemoryIndexShown(entries []store.Memory) int {
	_, lines := memoryIndexLines(entries, true)
	return len(lines)
}

// memoryIndexLines: titlul si randurile care incap in limite (40 de randuri, 3000 de caractere cu loc pentru "N more").
func memoryIndexLines(entries []store.Memory, withTools bool) (string, []string) {
	head := "Project memory (decisions and facts saved by the team):"
	if withTools {
		head = "Project memory (decisions and facts saved by the team; read the full text with mcp__team__recall):"
	}
	size := len([]rune(head))
	var lines []string
	for _, m := range entries {
		if len(lines) == memoryIndexMax {
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
		lines = append(lines, line)
		size += n
	}
	return head, lines
}
