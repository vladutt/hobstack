package skillsrc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"time"

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

// sourceRecheck: cat timp o sursa verificata nu se mai verifica (verificarea zilnica).
const sourceRecheck = 24 * time.Hour

// CheckSources verifica sursele elementelor din Library care n-au fost verificate de peste 24 h si
// salveaza referinta de acum (latest). Aceleasi reguli ca POST .../source/check:
//   - ErrRateLimited: nimic nou; bucla se opreste fara sa marcheze restul (urmatoarea incercare, peste o ora);
//   - ErrNotFound: sursa a disparut; se marcheaza verificata cu latest neschimbat (nu se reincearca azi);
//   - alte erori (GitHub nu raspunde): se logheaza, sursa ramane nemarcata si se continua cu urmatoarea.
func CheckSources(ctx context.Context, st *store.Store, r *Remote, now time.Time) error {
	list, err := st.ListLibrarySources()
	if err != nil {
		return err
	}
	for _, src := range list {
		if err := ctx.Err(); err != nil {
			return err
		}
		if src.CheckedAt != nil && now.Sub(*src.CheckedAt) < sourceRecheck {
			continue
		}
		latest, err := CheckSource(ctx, r, src)
		switch {
		case err == nil:
			if err := st.MarkSourceChecked(src.ItemID, latest, now); err != nil {
				return err
			}
		case errors.Is(err, ErrRateLimited):
			return nil
		case errors.Is(err, ErrNotFound):
			if err := st.MarkSourceChecked(src.ItemID, src.Latest, now); err != nil {
				return err
			}
		default:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("skill sources: item %d: %v", src.ItemID, err)
		}
	}
	return nil
}
