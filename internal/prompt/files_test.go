package prompt

import (
	"fmt"
	"strings"
	"testing"
)

func TestFilesBlock(t *testing.T) {
	if FilesBlock(nil) != "" {
		t.Fatal("lista goala trebuie sa dea text gol")
	}
	got := FilesBlock([]FileRef{{Name: "spec.pdf", Path: "/f/aa-spec.pdf"}})
	if got != "Project files (you can read them with Read):\n- /f/aa-spec.pdf (spec.pdf)" {
		t.Fatalf("FilesBlock = %q", got)
	}
	var many []FileRef
	for i := 0; i < 53; i++ {
		many = append(many, FileRef{Name: fmt.Sprint(i), Path: fmt.Sprint("/f/", i)})
	}
	got = FilesBlock(many)
	if strings.Count(got, "\n- /f/") != 50 || !strings.HasSuffix(got, "- ... and 3 more files") {
		t.Fatalf("limita de 50: %q", got)
	}
}
