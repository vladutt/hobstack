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
// un abonat cu buffer-ul plin (browser care nu mai citeste: conexiune moarta, tab inghetat) e scos si
// channel-ul lui inchis, ca stream-ul SSE sa se termine; browserul se reconecteaza si reincarca starea
// completa. Inainte pierdea evenimentul in tacere si chat-ul ramanea cu mesaje "queued".
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
			h.remove(projectID, ch)
		}
	}
}

// remove scoate abonatul si ii inchide channel-ul; se cheama sub h.mu.
// Doar cine il gaseste in map il inchide, deci nu se inchide de doua ori.
func (h *Hub) remove(projectID int64, ch chan []byte) {
	if _, ok := h.subs[projectID][ch]; !ok {
		return
	}
	delete(h.subs[projectID], ch)
	if len(h.subs[projectID]) == 0 {
		delete(h.subs, projectID)
	}
	close(ch)
}

// Subscribe inregistreaza un abonat. Functia intoarsa il scoate si inchide
// channel-ul; e sigur sa fie apelata de mai multe ori (si dupa ce Publish l-a scos).
func (h *Hub) Subscribe(projectID int64) (<-chan []byte, func()) {
	ch := make(chan []byte, buffer)
	h.mu.Lock()
	if h.subs[projectID] == nil {
		h.subs[projectID] = map[chan []byte]struct{}{}
	}
	h.subs[projectID][ch] = struct{}{}
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		h.remove(projectID, ch)
		h.mu.Unlock()
	}
	return ch, cancel
}
