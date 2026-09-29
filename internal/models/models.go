// Package models tine lista modelelor Claude pe care le poate folosi un agent.
// E singura sursa: serverul valideaza dupa ea, iar UI-ul o primeste din /api/info.
package models

const (
	// GroupLatest: alias-uri pe care Claude Code le traduce mereu in cea mai
	// noua versiune din familie (ex. "opus" = cel mai nou Opus).
	GroupLatest = "latest"
	// GroupPinned: ID-uri exacte; agentul ramane pe versiunea aleasa.
	GroupPinned = "pinned"
)

type Model struct {
	ID    string `json:"id"`    // ce primeste claude la --model
	Label string `json:"label"` // ce vede utilizatorul
	Group string `json:"group"`
}

// All e lista afisata in UI, in ordinea asta. Cand apare un model nou, il
// adaugi aici (ID-ul exact, fara sufix de data).
var All = []Model{
	{ID: "opus", Label: "Opus (cel mai nou)", Group: GroupLatest},
	{ID: "sonnet", Label: "Sonnet (cel mai nou)", Group: GroupLatest},
	{ID: "haiku", Label: "Haiku (cel mai nou)", Group: GroupLatest},
	{ID: "fable", Label: "Fable (cel mai nou)", Group: GroupLatest},

	{ID: "claude-fable-5-1", Label: "Fable 5.1", Group: GroupPinned},
	{ID: "claude-fable-5", Label: "Fable 5", Group: GroupPinned},
	{ID: "claude-opus-5-5", Label: "Opus 5.5", Group: GroupPinned},
	{ID: "claude-opus-5", Label: "Opus 5", Group: GroupPinned},
	{ID: "claude-opus-4-8", Label: "Opus 4.8", Group: GroupPinned},
	{ID: "claude-opus-4-7", Label: "Opus 4.7", Group: GroupPinned},
	{ID: "claude-opus-4-6", Label: "Opus 4.6", Group: GroupPinned},
	{ID: "claude-sonnet-5-5", Label: "Sonnet 5.5", Group: GroupPinned},
	{ID: "claude-sonnet-5", Label: "Sonnet 5", Group: GroupPinned},
	{ID: "claude-sonnet-4-6", Label: "Sonnet 4.6", Group: GroupPinned},
	{ID: "claude-haiku-4-5", Label: "Haiku 4.5", Group: GroupPinned},
}

// Valid spune daca id-ul e in lista.
func Valid(id string) bool {
	for _, m := range All {
		if m.ID == id {
			return true
		}
	}
	return false
}
