package main

import (
	"math"
	"strings"
	"testing"

	"hobstack/internal/appprefs"
)

func yes(winRect) bool { return true }
func no(winRect) bool  { return false }

func TestWindowRectDefault(t *testing.T) {
	got := windowRect(nil, winRect{0, 0, 1920, 1040}, 1, yes)
	if want := (winRect{260, 70, 1400, 900}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestWindowRectScaledAndClamped(t *testing.T) {
	// 1400x900 la 150% nu incape pe 1280x680: ia toata zona de lucru
	got := windowRect(nil, winRect{0, 0, 1280, 680}, 1.5, yes)
	if want := (winRect{0, 0, 1280, 680}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// zona de lucru mutata (bara de activitati sus / alt monitor)
	got = windowRect(nil, winRect{100, 40, 2000, 1000}, 1, yes)
	if want := (winRect{400, 90, 1400, 900}); got != want {
		t.Fatalf("offset: got %+v, want %+v", got, want)
	}
}

func TestWindowRectSaved(t *testing.T) {
	saved := &appprefs.Frame{X: 100, Y: 100, W: 800, H: 600}
	got := windowRect(saved, winRect{0, 0, 1920, 1040}, 1, yes)
	if want := (winRect{100, 100, 800, 600}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestWindowRectSavedOffScreen(t *testing.T) {
	saved := &appprefs.Frame{X: 3000, Y: 100, W: 800, H: 600} // monitorul al doilea nu mai e
	got := windowRect(saved, winRect{0, 0, 1920, 1040}, 1, no)
	if want := (winRect{260, 70, 1400, 900}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestWindowRectSavedTooSmall(t *testing.T) {
	saved := &appprefs.Frame{X: 10, Y: 10, W: 300, H: 200}
	got := windowRect(saved, winRect{0, 0, 1920, 1040}, 1, yes)
	if want := (winRect{260, 70, 1400, 900}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestClampZoom(t *testing.T) {
	for in, want := range map[float64]float64{0: 1, 0.2: 0.5, 7: 3, 1.25: 1.25, -1: 1} {
		if got := clampZoom(in); got != want {
			t.Errorf("clampZoom(%v) = %v, want %v", in, got, want)
		}
	}
	if got := clampZoom(math.NaN()); got != 1 {
		t.Errorf("NaN -> %v", got)
	}
}

func TestParseBridge(t *testing.T) {
	const origin = "http://127.0.0.1:7777"
	long := strings.Repeat("ă", 250)
	cases := []struct {
		raw, action, arg string
	}{
		{`{"type":"open","url":"https://github.com/x/y"}`, "open", "https://github.com/x/y"},
		{`{"type":"open","url":"mailto:a@b.c"}`, "open", "mailto:a@b.c"},
		{`{"type":"open","url":"http://127.0.0.1:7777/#/settings"}`, "", ""}, // pagina noastra: nu in browser
		{`{"type":"open","url":"javascript:alert(1)"}`, "", ""},
		{`{"type":"open","url":"file:///C:/Windows/notepad.exe"}`, "", ""},
		{`{"type":"open","url":"https://"}`, "", ""},
		{`{"type":"title","title":"  Board · HobStack  "}`, "title", "Board · HobStack"},
		{`{"type":"title","title":"` + long + `"}`, "title", strings.Repeat("ă", 200)},
		{`{"type":"other"}`, "", ""},
		{`not json`, "", ""},
	}
	for _, c := range cases {
		action, arg := parseBridge(c.raw, origin)
		if action != c.action || arg != c.arg {
			t.Errorf("parseBridge(%s) = %q, %q; want %q, %q", c.raw, action, arg, c.action, c.arg)
		}
	}
}
