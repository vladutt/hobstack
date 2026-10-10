package library

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"hobstack/internal/store"
)

func textFiles(n int, size int) []TextFile {
	var fs []TextFile
	for i := 0; i < n; i++ {
		fs = append(fs, TextFile{Path: fmt.Sprintf("f%d.txt", i), Content: strings.Repeat("x", size)})
	}
	return fs
}

func TestLimitsPerKind(t *testing.T) {
	if f, b := Limits(store.KindSkill); f != 200 || b != 10<<20 {
		t.Fatalf("skill: %d %d", f, b)
	}
	for _, k := range []string{store.KindTool, store.KindMCP} {
		if f, b := Limits(k); f != 20 || b != 1<<20 {
			t.Fatalf("%s: %d %d", k, f, b)
		}
	}
}

func TestWriteStagedLimitsByKind(t *testing.T) {
	_, lib, _ := newEnv(t)
	// skill: 150 de fisiere si 5 MB trec
	s, n, _, err := lib.WriteStaged(store.KindSkill, textFiles(150, 35000))
	if err != nil || n != 150 {
		t.Fatalf("150 fisiere: %v %d", err, n)
	}
	os.RemoveAll(s)
	s, _, _, err = lib.WriteStaged(store.KindSkill, textFiles(5, 1<<20))
	if err != nil {
		t.Fatalf("5 MB: %v", err)
	}
	os.RemoveAll(s)
	// skill: 201 de fisiere si 11 MB nu
	if _, _, _, err = lib.WriteStaged(store.KindSkill, textFiles(201, 1)); err == nil {
		t.Fatal("201 fisiere acceptate")
	}
	if _, _, _, err = lib.WriteStaged(store.KindSkill, textFiles(11, 1<<20)); err == nil {
		t.Fatal("11 MB acceptati")
	}
	// tool: 21 de fisiere nu
	if _, _, _, err = lib.WriteStaged(store.KindTool, textFiles(21, 1)); err == nil {
		t.Fatal("tool cu 21 de fisiere acceptat")
	}
}

func TestCopyStagedSkillLimit(t *testing.T) {
	_, lib, src := newEnv(t)
	var names []string
	for i := 0; i < 30; i++ {
		n := fmt.Sprintf("f%d.txt", i)
		os.WriteFile(src+"/"+n, []byte("x"), 0o644)
		names = append(names, n)
	}
	s, _, _, err := lib.CopyStaged(store.KindSkill, src, names)
	if err != nil {
		t.Fatalf("skill cu 30: %v", err)
	}
	os.RemoveAll(s)
	if _, _, _, err := lib.CopyStaged(store.KindTool, src, names); err == nil {
		t.Fatal("tool cu 30 acceptat")
	}
}
