package store

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanFileName(t *testing.T) {
	cases := map[string]string{
		"poza.png":         "poza.png",
		"../../etc/passwd": "passwd",
		`C:\\tmp\\a.txt`:   "a.txt",
		"a/b.png":          "b.png",
		"":                 "fisier",
		"..":               "fisier",
		"ra:u*?.txt":       "ra_u__.txt",
		"linie\nnoua.txt":  "linie_noua.txt",
	}
	for in, want := range cases {
		if got := CleanFileName(in); got != want {
			t.Errorf("CleanFileName(%q) = %q, vreau %q", in, got, want)
		}
	}
	long := CleanFileName(strings.Repeat("x", 300) + ".png")
	if !strings.HasSuffix(long, ".png") || len(long) > 200 {
		t.Errorf("nume lung: %d bytes, %q", len(long), long[len(long)-8:])
	}
	ro := CleanFileName(strings.Repeat("ș", 150) + ".pdf")
	if !utf8.ValidString(ro) || !strings.HasSuffix(ro, ".pdf") || len(ro) > 200 {
		t.Errorf("nume utf-8: valid=%v bytes=%d", utf8.ValidString(ro), len(ro))
	}
	if got := CleanFileName(".png"); got != "fisier.png" {
		t.Errorf(".png -> %q", got)
	}
	a, b := StoredName("x.png"), StoredName("x.png")
	if a == b || !strings.HasSuffix(a, "-x.png") || len(a) != 16+1+len("x.png") {
		t.Errorf("StoredName: %q %q", a, b)
	}
}

func TestAttachmentsLifecycle(t *testing.T) {
	s := newStore(t)
	p, lead := newProject(t, s)
	doc := Attachment{ProjectID: p.ID, Kind: KindProjectFile, Name: "spec.pdf", Stored: "aa-spec.pdf", Size: 10, Mime: "application/pdf"}
	img := Attachment{ProjectID: p.ID, Kind: KindMessageFile, Name: "bug.png", Stored: "bb-bug.png", Size: 5, Mime: "image/png"}
	for _, a := range []*Attachment{&doc, &img} {
		if err := s.AddAttachment(a); err != nil {
			t.Fatal(err)
		}
	}
	files, err := s.ListProjectFiles(p.ID)
	if err != nil || len(files) != 1 || files[0].Name != "spec.pdf" {
		t.Fatalf("ListProjectFiles = %+v, %v", files, err)
	}

	m := Message{ProjectID: p.ID, AgentID: lead.ID, Kind: KindMsg, Author: AuthorUser, Text: "vezi poza"}
	s.AddMessage(&m)
	if err := s.LinkAttachments([]int64{img.ID, doc.ID}, m.ID); err != nil {
		t.Fatal(err)
	}
	msgs, _ := s.ListMessages(lead.ID)
	if len(msgs) != 1 || len(msgs[0].Attachments) != 1 || msgs[0].Attachments[0].Name != "bug.png" {
		t.Fatalf("mesajul trebuia sa aiba doar poza (fisierul de proiect nu se leaga): %+v", msgs)
	}
	pending, _ := s.Undelivered(lead.ID)
	if len(pending) != 1 || len(pending[0].Attachments) != 1 {
		t.Fatalf("Undelivered fara atasamente: %+v", pending)
	}
	plain := Message{ProjectID: p.ID, AgentID: lead.ID, Kind: KindEvent, Text: "x"}
	s.AddMessage(&plain)
	msgs, _ = s.ListMessages(lead.ID)
	if msgs[1].Attachments == nil {
		t.Fatal("Attachments trebuie sa fie [] nu nil")
	}

	if err := s.DeleteAttachment(doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAttachment(doc.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dupa stergere: %v", err)
	}
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAttachment(img.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("atasamentele trebuiau sterse in cascada cu proiectul")
	}
}
