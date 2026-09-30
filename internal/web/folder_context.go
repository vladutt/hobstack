package web

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// maxFolderContext e aceeasi limita ca pentru "Context for this folder".
const maxFolderContext = 20000

// folderContext: GET /api/folder-context?path=... citeste CLAUDE.md din folder,
// ca formularul sa completeze "Context for this folder". Citeste doar acest
// fisier; un folder sau un CLAUDE.md lipsa inseamna found=false, nu eroare
// (folderul poate fi creat abia la salvarea proiectului).
func (s *Server) folderContext(w http.ResponseWriter, r *http.Request) {
	dir, err := expandHome(strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		fail(w, err)
		return
	}
	if dir == "" || !filepath.IsAbs(dir) {
		fail(w, badRequest("the folder must be an absolute path, e.g. /Users/you/project or ~/project"))
		return
	}
	body, err := os.ReadFile(filepath.Join(filepath.Clean(dir), "CLAUDE.md"))
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "content": "", "truncated": false})
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	content := strings.TrimSpace(string(body))
	truncated := utf8.RuneCountInString(content) > maxFolderContext
	if truncated {
		content = string([]rune(content)[:maxFolderContext])
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": true, "content": content, "truncated": truncated})
}
