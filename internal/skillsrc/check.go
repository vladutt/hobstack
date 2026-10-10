package skillsrc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"hobstack/internal/store"
)

// CheckSource intoarce referinta de acum a sursei unui element: pentru github ultimul commit care atinge
// folderul skill-ului (radacina repo-ului: commit-ul HEAD), pentru sursele locale amprenta folderului.
// Folosita de POST /api/library/items/{id}/source/check si de verificarea zilnica.
//
// Erori: ErrRateLimited = "nimic nou, incearca mai tarziu" (fara eroare in UI); ErrNotFound = sursa nu mai
// exista (local: "Source no longer on this machine."); ErrUnavailable = GitHub nu raspunde.
func CheckSource(ctx context.Context, r *Remote, src store.LibrarySource) (string, error) {
	switch src.Kind {
	case store.SourceGitHub:
		if src.Path == "" {
			return r.HeadCommit(ctx, src.Repo)
		}
		return r.PathCommit(ctx, src.Repo, src.Path)
	case store.SourceClaudeCode, store.SourceClaudeAI, store.SourcePlugin:
		fp, err := Fingerprint(src.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return "", errorf(ErrNotFound, "Source no longer on this machine.")
		}
		return fp, err
	}
	return "", fmt.Errorf("unknown source kind %q", src.Kind)
}
