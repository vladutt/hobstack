// Package events trimite notificari live catre browserele deschise pe un
// proiect: task schimbat, mesaj nou, agent pornit/oprit. Transportul e SSE (in web).
package events

import (
	"encoding/json"
	"sync"
)

// buffer: cate evenimente poate avea in asteptare un abonat inainte sa pierdem.
const buffer = 64

type Hub struct {
	mu   sync.Mutex
	subs map[int64]map[chan []byte]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: map[int64]map[chan []byte]struct{}{}}
}

// Publish trimite {type, data} abonatilor proiectului. Nu blocheaza niciodata:
// un abonat lent pierde evenimentul, iar UI-ul oricum reincarca starea completa.
func (h *Hub) Publish(projectID int64, typ string, data any) {
	payload, err := json.Marshal(map[string]any{"type": typ, "data": data})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[projectID] {
		select {
		case ch <- payload:
		default:
		}
	}
}

// Subscribe inregistreaza un abonat. Functia intoarsa il scoate si inchide
// channel-ul; e sigur sa fie apelata de mai multe ori.
func (h *Hub) Subscribe(projectID int64) (<-chan []byte, func()) {
	ch := make(chan []byte, buffer)
	h.mu.Lock()
	if h.subs[projectID] == nil {
		h.subs[projectID] = map[chan []byte]struct{}{}
	}
	h.subs[projectID][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs[projectID], ch)
			if len(h.subs[projectID]) == 0 {
				delete(h.subs, projectID)
			}
			h.mu.Unlock()
			// inchidem dupa ce l-am scos, sub lock nu se mai trimite nimic pe el
			close(ch)
		})
	}
	return ch, cancel
}
