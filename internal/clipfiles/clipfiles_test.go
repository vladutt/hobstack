package clipfiles

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseLines(t *testing.T) {
	got := parseLines("/a/x.txt\r\n\n/b/my file.pdf\n")
	if strings.Join(got, "|") != "/a/x.txt|/b/my file.pdf" {
		t.Fatalf("%q", got)
	}
	if parseLines("") != nil {
		t.Fatal("empty output: want nil")
	}
	var many []string
	for i := 0; i < Max+5; i++ {
		many = append(many, fmt.Sprintf("/f%d", i))
	}
	if n := len(parseLines(strings.Join(many, "\n"))); n != Max {
		t.Fatalf("got %d, want %d", n, Max)
	}
}

func TestParseURIList(t *testing.T) {
	got := parseURIList("# comment\nfile:///home/u/my%20file.txt\r\nhttps://x.y/z\nfile:///tmp/b\n")
	if strings.Join(got, "|") != "/home/u/my file.txt|/tmp/b" {
		t.Fatalf("%q", got)
	}
}
