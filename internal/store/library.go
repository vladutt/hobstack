package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Tipurile de elemente din Library.
const (
	KindTool  = "tool"
	KindSkill = "skill"
	KindMCP   = "mcp"
)

// Starile unui element din Library.
const (
	ItemProposed    = "proposed"
	ItemPublished   = "published"
	ItemDeclined    = "declined"
	ItemUnpublished = "unpublished"
)

// LibraryItem e un tool, un skill sau un server MCP refolosit intre proiecte.
type LibraryItem struct {
	ID              int64      `json:"id"`
	Kind            string     `json:"kind"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Status          string     `json:"status"`
	SourceProjectID int64      `json:"sourceProjectId"`
	SourceTaskID    int64      `json:"sourceTaskId"`
	MadeByAgentID   int64      `json:"madeByAgentId"`
	MadeBy          string     `json:"madeBy"` // numele la momentul propunerii, "You" pentru utilizator
	Needs           []string   `json:"needs"`
	Secrets         []string   `json:"secrets"`
	Transport       string     `json:"transport"` // mcp: "local" | "remote"
	URL             string     `json:"url"`
	Command         string     `json:"command"`
	Args            []string   `json:"args"`
	Entry           string     `json:"entry"`     // tool: fisierul care se ruleaza
	ToolNames       []string   `json:"toolNames"` // mcp: tool-urile expuse (din probe)
	CreatedAt       time.Time  `json:"createdAt"`
	PublishedAt     *time.Time `json:"publishedAt"`
}

// LibraryVersion e o versiune salvata (pe disc, in Dir) a unui element.
type LibraryVersion struct {
	ID        int64     `json:"id"`
	ItemID    int64     `json:"itemId"`
	Version   string    `json:"version"`
	Dir       string    `json:"dir"`
	Files     int       `json:"files"`
	Lines     int       `json:"lines"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
}

// LibraryInstall: un element instalat intr-un proiect, pe o versiune.
type LibraryInstall struct {
	ProjectID int64 `json:"projectId"`
	ItemID    int64 `json:"itemId"`
	VersionID int64 `json:"versionId"`
	Enabled   bool  `json:"enabled"`
}

// LibraryUse: o folosire a elementului intr-un task.
type LibraryUse struct {
	TaskID int64     `json:"taskId"`
	Number int       `json:"number"`
	Title  string    `json:"title"`
	Agent  string    `json:"agent"`
	At     time.Time `json:"at"`
}

const libraryItemCols = `id, kind, name, description, status, source_project_id, source_task_id, made_by_agent_id, made_by,
	needs, secrets, transport, url, command, args, entry, tool_names, created_at, published_at`

// jsonList serializeaza o lista; nil devine [].
func jsonList(l []string) string {
	if l == nil {
		l = []string{}
	}
	b, _ := json.Marshal(l)
	return string(b)
}

// parseList decodeaza o lista JSON; rezultatul nu e niciodata nil.
func parseList(s string) []string {
	l := []string{}
	_ = json.Unmarshal([]byte(s), &l)
	if l == nil {
		l = []string{}
	}
	return l
}

func scanLibraryItem(sc scanner) (LibraryItem, error) {
	var it LibraryItem
	var src, task, agent sql.NullInt64
	var needs, secrets, args, tools, created string
	var published sql.NullString
	err := sc.Scan(&it.ID, &it.Kind, &it.Name, &it.Description, &it.Status, &src, &task, &agent, &it.MadeBy,
		&needs, &secrets, &it.Transport, &it.URL, &it.Command, &args, &it.Entry, &tools, &created, &published)
	if err == sql.ErrNoRows {
		return it, ErrNotFound
	}
	if err != nil {
		return it, err
	}
	it.SourceProjectID, it.SourceTaskID, it.MadeByAgentID = src.Int64, task.Int64, agent.Int64
	it.Needs, it.Secrets, it.Args, it.ToolNames = parseList(needs), parseList(secrets), parseList(args), parseList(tools)
	it.CreatedAt = parseTime(created)
	if published.Valid {
		t := parseTime(published.String)
		it.PublishedAt = &t
	}
	return it, nil
}

// AddLibraryItem insereaza elementul si ii completeaza id-ul si data.
// ErrConflict daca exista deja acelasi kind si nume.
func (s *Store) AddLibraryItem(it *LibraryItem) error {
	created := now()
	var published any
	if it.Status == ItemPublished {
		published = created
	}
	res, err := s.db.Exec(`INSERT INTO library_items (kind, name, description, status, source_project_id, source_task_id,
		made_by_agent_id, made_by, needs, secrets, transport, url, command, args, entry, tool_names, created_at, published_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.Kind, it.Name, it.Description, it.Status, nullID(it.SourceProjectID), nullID(it.SourceTaskID),
		nullID(it.MadeByAgentID), it.MadeBy, jsonList(it.Needs), jsonList(it.Secrets), it.Transport, it.URL, it.Command,
		jsonList(it.Args), it.Entry, jsonList(it.ToolNames), created, published)
	if isUnique(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	it.ID, _ = res.LastInsertId()
	it.CreatedAt = parseTime(created)
	if published != nil {
		t := it.CreatedAt
		it.PublishedAt = &t
	}
	return nil
}

// GetLibraryItem intoarce elementul dupa id. ErrNotFound daca nu exista.
func (s *Store) GetLibraryItem(id int64) (LibraryItem, error) {
	return scanLibraryItem(s.db.QueryRow(`SELECT `+libraryItemCols+` FROM library_items WHERE id = ?`, id))
}

// FindLibraryItem cauta dupa kind si nume. ErrNotFound daca nu exista.
func (s *Store) FindLibraryItem(kind, name string) (LibraryItem, error) {
	return scanLibraryItem(s.db.QueryRow(`SELECT `+libraryItemCols+` FROM library_items WHERE kind = ? AND name = ?`, kind, name))
}

// ListLibraryItems: kind "" = toate; fara declined; propunerile primele, apoi dupa nume.
func (s *Store) ListLibraryItems(kind string) ([]LibraryItem, error) {
	rows, err := s.db.Query(`SELECT `+libraryItemCols+` FROM library_items
		WHERE status <> 'declined' AND (? = '' OR kind = ?)
		ORDER BY status = 'proposed' DESC, name COLLATE NOCASE, id`, kind, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LibraryItem{}
	for rows.Next() {
		it, err := scanLibraryItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// SetLibraryStatus schimba starea; published_at se pune doar la primul published.
func (s *Store) SetLibraryStatus(id int64, status string) error {
	return mustAffect(s.db.Exec(`UPDATE library_items SET status = ?,
		published_at = CASE WHEN ? = 'published' AND published_at IS NULL THEN ? ELSE published_at END
		WHERE id = ?`, status, status, now(), id))
}

// AddLibraryVersion insereaza o versiune. ErrConflict daca versiunea exista deja.
func (s *Store) AddLibraryVersion(v *LibraryVersion) error {
	created := now()
	res, err := s.db.Exec(`INSERT INTO library_versions (item_id, version, dir, files, lines, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, v.ItemID, v.Version, v.Dir, v.Files, v.Lines, v.Note, created)
	if isUnique(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	v.ID, _ = res.LastInsertId()
	v.CreatedAt = parseTime(created)
	return nil
}

// ListLibraryVersions: cea mai noua prima.
func (s *Store) ListLibraryVersions(itemID int64) ([]LibraryVersion, error) {
	rows, err := s.db.Query(`SELECT id, item_id, version, dir, files, lines, note, created_at
		FROM library_versions WHERE item_id = ? ORDER BY id DESC`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LibraryVersion{}
	for rows.Next() {
		var v LibraryVersion
		var created string
		if err := rows.Scan(&v.ID, &v.ItemID, &v.Version, &v.Dir, &v.Files, &v.Lines, &v.Note, &created); err != nil {
			return nil, err
		}
		v.CreatedAt = parseTime(created)
		out = append(out, v)
	}
	return out, rows.Err()
}

// Install instaleaza (sau muta pe alta versiune) un element in proiect, activat.
func (s *Store) Install(projectID, itemID, versionID int64) error {
	_, err := s.db.Exec(`INSERT INTO library_installs (project_id, item_id, version_id, enabled) VALUES (?, ?, ?, 1)
		ON CONFLICT (project_id, item_id) DO UPDATE SET version_id = excluded.version_id, enabled = 1`,
		projectID, itemID, versionID)
	return err
}

// Uninstall scoate elementul din proiect. ErrNotFound daca nu era instalat.
func (s *Store) Uninstall(projectID, itemID int64) error {
	return mustAffect(s.db.Exec(`DELETE FROM library_installs WHERE project_id = ? AND item_id = ?`, projectID, itemID))
}

// ListInstalls intoarce instalarile unui proiect.
func (s *Store) ListInstalls(projectID int64) ([]LibraryInstall, error) {
	rows, err := s.db.Query(`SELECT project_id, item_id, version_id, enabled FROM library_installs
		WHERE project_id = ? ORDER BY item_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LibraryInstall{}
	for rows.Next() {
		var in LibraryInstall
		if err := rows.Scan(&in.ProjectID, &in.ItemID, &in.VersionID, &in.Enabled); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// SetSecret salveaza (sau schimba) valoarea unui secret al elementului in proiect.
func (s *Store) SetSecret(projectID, itemID int64, name, value string) error {
	_, err := s.db.Exec(`INSERT INTO library_secrets (project_id, item_id, name, value) VALUES (?, ?, ?, ?)
		ON CONFLICT (project_id, item_id, name) DO UPDATE SET value = excluded.value`, projectID, itemID, name, value)
	return err
}

// Secrets intoarce secretele elementului in proiect (harta goala daca nu are).
func (s *Store) Secrets(projectID, itemID int64) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT name, value FROM library_secrets WHERE project_id = ? AND item_id = ?`, projectID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// CountOpenProposals: propunerile in asteptare facute din proiect.
func (s *Store) CountOpenProposals(projectID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM library_items WHERE status = 'proposed' AND source_project_id = ?`, projectID).Scan(&n)
	return n, err
}

// AddLibraryUse noteaza ca agentul a folosit elementul in task.
func (s *Store) AddLibraryUse(itemID, taskID, agentID int64) error {
	_, err := s.db.Exec(`INSERT INTO library_uses (item_id, task_id, agent_id, at) VALUES (?, ?, ?, ?)`,
		itemID, nullID(taskID), nullID(agentID), now())
	return err
}

// RecentLibraryUses intoarce ultimele n folosiri (numarul si titlul task-ului, numele agentului), cele mai noi primele.
func (s *Store) RecentLibraryUses(itemID int64, n int) ([]LibraryUse, error) {
	rows, err := s.db.Query(`SELECT t.id, t.number, t.title, COALESCE(a.name, ''), u.at
		FROM library_uses u JOIN tasks t ON t.id = u.task_id LEFT JOIN agents a ON a.id = u.agent_id
		WHERE u.item_id = ? ORDER BY u.at DESC, u.id DESC LIMIT ?`, itemID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LibraryUse{}
	for rows.Next() {
		var u LibraryUse
		var at string
		if err := rows.Scan(&u.TaskID, &u.Number, &u.Title, &u.Agent, &at); err != nil {
			return nil, err
		}
		u.At = parseTime(at)
		out = append(out, u)
	}
	return out, rows.Err()
}

// DeleteLibraryItem sterge un element (si ce atarna de el); folosit cand o propunere esueaza dupa ce a primit id.
func (s *Store) DeleteLibraryItem(id int64) error {
	_, err := s.db.Exec(`DELETE FROM library_items WHERE id = ?`, id)
	return err
}

// SetLibraryToolNames salveaza tool-urile expuse de un server MCP (rezultatul probe-ului de la Publish).
func (s *Store) SetLibraryToolNames(id int64, names []string) error {
	_, err := s.db.Exec(`UPDATE library_items SET tool_names = ? WHERE id = ?`, jsonList(names), id)
	return err
}
