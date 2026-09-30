package prompt

import (
	"fmt"
	"strings"
)

// maxFiles limiteaza cate fisiere de proiect apar in mesajul turei.
const maxFiles = 50

// FileRef e un fisier de proiect: numele afisat si calea de pe disc.
type FileRef struct {
	Name string
	Path string
}

// FilesBlock listeaza fisierele proiectului pentru agent. Fara fisiere intoarce "".
func FilesBlock(files []FileRef) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Project files (you can read them with Read):")
	for i, f := range files {
		if i == maxFiles {
			fmt.Fprintf(&b, "\n- ... and %d more files", len(files)-maxFiles)
			break
		}
		fmt.Fprintf(&b, "\n- %s (%s)", f.Path, f.Name)
	}
	return b.String()
}
