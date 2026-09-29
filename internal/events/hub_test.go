package events

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPublishOnlyToProjectSubscribers(t *testing.T) {
	h := NewHub()
	a, cancelA := h.Subscribe(1)
	defer cancelA()
	b, cancelB := h.Subscribe(2)
	defer cancelB()

	h.Publish(1, "task", map[string]int{"id": 7})

	select {
	case raw := <-a:
		var ev struct {
			Type string         `json:"type"`
			Data map[string]int `json:"data"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil || ev.Type != "task" || ev.Data["id"] != 7 {
			t.Fatalf("eveniment = %s (%v)", raw, err)
		}
	case <-time.After(time.Second):
		t.Fatal("abonatul proiectului 1 nu a primit nimic")
	}
	select {
	case raw := <-b:
		t.Fatalf("abonatul proiectului 2 a primit %s", raw)
	default:
	}
}

func TestCancelClosesChannel(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe(1)
	cancel()
	cancel() // a doua oara nu trebuie sa dea panic
	if _, ok := <-ch; ok {
		t.Fatal("channel-ul trebuia inchis")
	}
	h.Publish(1, "task", nil) // fara abonati: nu trebuie sa dea panic
}

func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe(1)
	defer cancel()
	finished := make(chan struct{})
	go func() {
		for i := 0; i < buffer*3; i++ { // nimeni nu citeste
			h.Publish(1, "message", i)
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish a blocat din cauza unui abonat lent")
	}
	if len(ch) != buffer {
		t.Fatalf("in buffer: %d, vreau %d", len(ch), buffer)
	}
}
