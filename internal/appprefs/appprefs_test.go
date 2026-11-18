package appprefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingIsWindow(t *testing.T) {
	if got := Load(t.TempDir()).OpenIn; got != OpenWindow {
		t.Fatalf("openIn = %q", got)
	}
}

func TestLoadCorruptIsDefault(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "app.json"), []byte(`{"openIn":`), 0o644)
	if got := Load(root).OpenIn; got != OpenWindow {
		t.Fatalf("openIn = %q", got)
	}
}

func TestLoadUnknownOpenIn(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "app.json"), []byte(`{"openIn":"tab"}`), 0o644)
	if got := Load(root).OpenIn; got != OpenWindow {
		t.Fatalf("openIn = %q", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	in := Prefs{OpenIn: OpenBrowser, Frame: &Frame{10, 20, 1200, 800}, Zoom: 1.25}
	if err := Save(root, in); err != nil {
		t.Fatal(err)
	}
	got := Load(root)
	if got.OpenIn != OpenBrowser || got.Frame == nil || *got.Frame != *in.Frame || got.Zoom != 1.25 {
		t.Fatalf("%+v", got)
	}
}

func TestUpdateKeepsOtherFields(t *testing.T) {
	root := t.TempDir()
	Save(root, Prefs{OpenIn: OpenWindow, Frame: &Frame{1, 2, 3, 4}})
	if err := Update(root, func(p *Prefs) { p.OpenIn = OpenBrowser }); err != nil {
		t.Fatal(err)
	}
	got := Load(root)
	if got.OpenIn != OpenBrowser || got.Frame == nil || *got.Frame != (Frame{1, 2, 3, 4}) {
		t.Fatalf("%+v", got)
	}
}
