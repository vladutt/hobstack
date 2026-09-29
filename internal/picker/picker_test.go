package picker

import (
	"errors"
	"testing"
)

func TestParseResult(t *testing.T) {
	if p, err := parseResult("/Users/x/Proiect/\n", "", nil); err != nil || p != "/Users/x/Proiect" {
		t.Fatalf("%q %v", p, err)
	}
	if p, _ := parseResult("/\n", "", nil); p != "/" {
		t.Fatalf("radacina: %q", p)
	}
	if _, err := parseResult("", "execution error: User canceled. (-128)", errors.New("exit 1")); !errors.Is(err, ErrCanceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := parseResult("", "", nil); !errors.Is(err, ErrCanceled) {
		t.Fatal("rezultat gol = cancel")
	}
	if _, err := parseResult("", "altceva", errors.New("exit 2")); err == nil || errors.Is(err, ErrCanceled) {
		t.Fatalf("eroare reala: %v", err)
	}
}
