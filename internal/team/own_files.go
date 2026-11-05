package team

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"hobstack/internal/mcp"
	"hobstack/internal/store"
)

// Fisierele unui agent: ce a creat el insusi (task_changes, kind create: Write pe un fisier nou sau un fisier nou
// prin shell). Le poate sterge fara sa intrebe utilizatorul: prin tool-ul delete_files sau, daca a incercat
// `rm -rf` si regula Ask l-a oprit, HobStack le sterge in locul lui (recordDenials). Orice altceva ramane la
// utilizator (cardul din Needs you).

// maxOwnFiles: un folder cu mai multe fisiere nu se sterge automat (verificarea ar dura si e suspect).
const maxOwnFiles = 5000

// notOwnError: o cale care nu e a agentului; mesajul ajunge la agent.
type notOwnError struct{ msg string }

func (e notOwnError) Error() string { return e.msg }

// ownTargets verifica toate caile (relative la folderul agentului sau absolute in el) si intoarce cele care exista,
// absolute. Orice cale care nu e a agentului opreste tot (nu stergem jumatate).
func (t *Team) ownTargets(a store.Agent, home string, paths []string) ([]string, error) {
	if home == "" {
		return nil, errors.New("your folder is unknown")
	}
	if len(paths) == 0 {
		return nil, errors.New("paths: the files or folders to delete, e.g. [\".scratch\"]")
	}
	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return nil, err
	}
	created, err := t.st.AgentCreatedPaths(a.ID)
	if err != nil {
		return nil, err
	}
	var mkdirs []string // incarcate doar daca e nevoie (foldere goale)
	var out []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(home, p)
		}
		abs = filepath.Clean(abs)
		rel, err := filepath.Rel(filepath.Clean(home), abs)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, notOwnError{p + " is not inside your folder"}
		}
		info, err := os.Lstat(abs)
		if errors.Is(err, fs.ErrNotExist) && !filepath.IsAbs(p) {
			// comanda a rulat dintr-un subfolder (cd mai devreme): cautam calea printre cele create de agent
			if sub, ok := uniqueCreatedDir(created, filepath.ToSlash(filepath.Clean(p))); ok {
				abs = filepath.Join(home, filepath.FromSlash(sub))
				rel = filepath.FromSlash(sub)
				info, err = os.Lstat(abs)
			}
		}
		if errors.Is(err, fs.ErrNotExist) {
			continue // deja sters
		}
		if err != nil {
			return nil, err
		}
		// un symlink pe drum ar putea duce in afara folderului
		parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
		if err != nil {
			return nil, err
		}
		if r, err := filepath.Rel(realHome, parent); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return nil, notOwnError{p + " is not inside your folder"}
		}
		relSlash := filepath.ToSlash(rel)
		if !info.IsDir() {
			if !created[relSlash] {
				return nil, notOwnError{p + " was not created by you"}
			}
		} else {
			files := 0
			werr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				files++
				if files > maxOwnFiles {
					return notOwnError{fmt.Sprintf("%s has more than %d files", p, maxOwnFiles)}
				}
				r, _ := filepath.Rel(filepath.Clean(home), path)
				if !created[filepath.ToSlash(r)] {
					return notOwnError{filepath.ToSlash(r) + " (in " + p + ") was not created by you"}
				}
				return nil
			})
			if werr != nil {
				return nil, werr
			}
			if files == 0 {
				// un folder gol: al lui daca a avut fisiere create de el sau l-a facut cu mkdir
				if mkdirs == nil {
					if mkdirs, err = t.st.AgentMkdirCommands(a.ID); err != nil {
						return nil, err
					}
					if mkdirs == nil {
						mkdirs = []string{}
					}
				}
				if !hadFilesIn(created, relSlash) && !madeDir(mkdirs, relSlash) {
					return nil, notOwnError{p + " was not created by you"}
				}
			}
		}
		if tracked(home, rel) {
			return nil, notOwnError{p + " is tracked by git; HobStack only deletes files that are not committed"}
		}
		out = append(out, abs)
	}
	return out, nil
}

// uniqueCreatedDir: singura cale creata de agent care se termina in p (fisier) sau il are ca folder
// (".scratch" -> "app/.scratch" din "app/.scratch/t.mjs"). Doua potriviri = ambiguu, false.
func uniqueCreatedDir(created map[string]bool, p string) (string, bool) {
	found := map[string]bool{}
	for c := range created {
		if strings.HasSuffix(c, "/"+p) {
			found[c] = true
		}
		if i := strings.Index(c, "/"+p+"/"); i >= 0 {
			found[c[:i+1+len(p)]] = true
		}
	}
	if len(found) != 1 {
		return "", false
	}
	for f := range found {
		return f, true
	}
	return "", false
}

func hadFilesIn(created map[string]bool, dir string) bool {
	for p := range created {
		if strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}

// madeDir: o comanda mkdir a agentului numeste folderul (de ex. `mkdir -p .scratch` sau `mkdir .scratch/out`).
func madeDir(cmds []string, dir string) bool {
	for _, c := range cmds {
		for _, f := range strings.Fields(c) {
			f = strings.Trim(strings.TrimSuffix(strings.Trim(f, `"'`), "/"), "./")
			if f == strings.Trim(dir, "./") {
				return true
			}
		}
	}
	return false
}

// tracked: calea (sau ceva din ea) e in git. Fara repo git sau fara git: nimic urmarit.
func tracked(home, rel string) bool {
	cmd := exec.Command("git", "-C", home, "ls-files", "--", rel)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// deleteOwn sterge caile verificate si trece stergerea in istoricul task-ului.
func (t *Team) deleteOwn(task *store.Task, a store.Agent, home string, abs []string) []string {
	var done []string
	for _, p := range abs {
		if err := os.RemoveAll(p); err != nil {
			continue
		}
		rel, _ := filepath.Rel(home, p)
		done = append(done, filepath.ToSlash(rel))
	}
	if task != nil && len(done) > 0 {
		t.taskEvent(*task, "deleted_files", a.Name, fmt.Sprintf("%s deleted %s (created by %s)", a.Name, strings.Join(done, ", "), a.Name))
	}
	return done
}

// DeleteFiles: tool-ul MCP delete_files. Sterge doar ce a creat agentul, in folderul lui, necommis in git.
func (t *Team) DeleteFiles(c mcp.Caller, paths []string) (string, error) {
	t.work.Lock()
	defer t.work.Unlock()
	if c.TaskID == 0 {
		return "", errors.New("delete_files works only while you work on a task")
	}
	a, err := t.st.GetAgent(c.AgentID)
	if err != nil {
		return "", err
	}
	p, err := t.st.GetProject(c.ProjectID)
	if err != nil {
		return "", err
	}
	home := store.HomeFolder(a, p.Folders).Path
	abs, err := t.ownTargets(a, home, paths)
	if err != nil {
		var no notOwnError
		if errors.As(err, &no) {
			return "", fmt.Errorf("%s. You can delete only files and folders you created yourself; leave the rest", no.msg)
		}
		return "", err
	}
	if len(abs) == 0 {
		return "Nothing to delete: those paths do not exist.", nil
	}
	var task *store.Task
	if tk, err := t.st.GetTask(c.TaskID); err == nil {
		task = &tk
	}
	done := t.deleteOwn(task, a, home, abs)
	if len(done) < len(abs) {
		return fmt.Sprintf("Deleted %s; some paths could not be removed.", strings.Join(done, ", ")), nil
	}
	return "Deleted " + strings.Join(done, ", ") + ".", nil
}

// rmTargets: caile dintr-o comanda `rm` simpla (flag-uri -r / -f / -v si cai, fara glob-uri, variabile sau alte
// comenzi legate cu ; && |). ok = false pentru orice altceva.
func rmTargets(cmd string) ([]string, bool) {
	cmd = strings.TrimSpace(cmd)
	if strings.ContainsAny(cmd, ";&|<>$`*?[]{}~\\\n") {
		return nil, false
	}
	var fields []string
	var cur strings.Builder
	quote := byte(0)
	inField := false
	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				cur.WriteByte(ch)
			}
		case ch == '\'' || ch == '"':
			quote, inField = ch, true
		case ch == ' ' || ch == '\t':
			if inField {
				fields = append(fields, cur.String())
				cur.Reset()
				inField = false
			}
		default:
			cur.WriteByte(ch)
			inField = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	if inField {
		fields = append(fields, cur.String())
	}
	if len(fields) < 2 || (fields[0] != "rm" && fields[0] != "/bin/rm") {
		return nil, false
	}
	var paths []string
	flags := true
	for _, f := range fields[1:] {
		if flags && f == "--" {
			flags = false
			continue
		}
		if flags && strings.HasPrefix(f, "-") {
			if slices.Contains([]string{"--recursive", "--force", "--verbose"}, f) {
				continue
			}
			if strings.HasPrefix(f, "--") || strings.Trim(f[1:], "rRfv") != "" || len(f) == 1 {
				return nil, false
			}
			continue
		}
		paths = append(paths, f)
	}
	return paths, len(paths) > 0
}
