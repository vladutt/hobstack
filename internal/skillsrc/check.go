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
// folderul skill-ului (radacina repo-ului: commit-ul HEAD), pentru sursele locale amprenta folderului de acum
// (la plugin-uri folderul versiunii instalate acum, vezi ResolveLocal; home = folderul utilizatorului).
// Folosita de POST /api/library/items/{id}/source/check si de verificarea zilnica.
//
// GitHub: daca la install GitHub a limitat cererea commits?path=&sha=, ref-ul salvat e HEAD-ul repo-ului de
// atunci, nu commit-ul de pe cale. Cand ultimul commit pe cale difera de ref, mai intrebam o data care e
// ultimul commit pe cale pana la ref: daca e acelasi, continutul nu s-a schimbat si intoarcem ref-ul (fara
// update fals). Ref-ul nu se rescrie (fara stare in plus); cererea in plus apare doar cand ref != ultimul commit.
//
// Erori: ErrRateLimited = "nimic nou, incearca mai tarziu" (fara eroare in UI); ErrNotFound = sursa nu mai
// exista (local: "Source no longer on this machine."); ErrUnavailable = GitHub nu raspunde.
func CheckSource(ctx context.Context, r *Remote, home string, src store.LibrarySource) (string, error) {
	switch src.Kind {
	case store.SourceGitHub:
		if src.Path == "" {
			return r.HeadCommit(ctx, src.Repo)
		}
		latest, err := r.PathCommit(ctx, src.Repo, src.Path)
		if err != nil || latest == src.Ref || !shaRe.MatchString(src.Ref) {
			return latest, err
		}
		at, err := r.PathCommitAt(ctx, src.Repo, src.Path, src.Ref)
		switch {
		case errors.Is(err, ErrNotFound):
			return latest, nil // ref-ul nu mai e in istorie: ce e acum e nou
		case err != nil:
			return "", err
		case at == latest:
			return src.Ref, nil
		}
		return latest, nil
	case store.SourceClaudeCode, store.SourceClaudeAI, store.SourcePlugin:
		dir, _, err := ResolveLocal(home, src)
		if err != nil {
			return "", err
		}
		fp, err := Fingerprint(dir)
		if errors.Is(err, fs.ErrNotExist) {
			return "", errorf(ErrNotFound, "Source no longer on this machine.")
		}
		return fp, err
	}
	return "", fmt.Errorf("unknown source kind %q", src.Kind)
}

// sourceRecheck: cat timp o sursa verificata nu se mai verifica (verificarea zilnica).
const sourceRecheck = 24 * time.Hour

// CheckSources verifica (home: ca la CheckSource) sursele elementelor din Library neverificate de peste 24 h si
// salveaza referinta de acum (latest). Aceleasi reguli ca POST .../source/check:
//   - ErrRateLimited: nimic nou; bucla se opreste fara sa marcheze restul (urmatoarea incercare, peste o ora);
//   - ErrNotFound: sursa a disparut; se marcheaza verificata cu latest neschimbat (nu se reincearca azi);
//   - alte erori (GitHub nu raspunde): se logheaza, sursa ramane nemarcata si se continua cu urmatoarea.
func CheckSources(ctx context.Context, st *store.Store, r *Remote, home string, now time.Time) error {
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
		latest, err := CheckSource(ctx, r, home, src)
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
