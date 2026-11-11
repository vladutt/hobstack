package store

import (
	"database/sql"
	"time"
)

// Starile unui card de brainstorming.
const (
	CardWaiting = "waiting"
	CardRunning = "running"
	CardDone    = "done"
	CardStopped = "stopped"
	CardFailed  = "failed"
	CardSkipped = "skipped"
)

// Felurile unui card terminat.
const (
	CardIdea     = "idea"
	CardWild     = "wild"
	CardQuestion = "question"
)

type BrainstormTopic struct {
	ID           int64     `json:"id"`
	ProjectID    int64     `json:"projectId"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Archived     bool      `json:"archived"`
	Participants []int64   `json:"participants"` // mereu non-nil
	CreatedAt    time.Time `json:"createdAt"`
}

type BrainstormRound struct {
	ID        int64     `json:"id"`
	TopicID   int64     `json:"topicId"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

type BrainstormCard struct {
	ID        int64     `json:"id"`
	TopicID   int64     `json:"topicId"`
	RoundID   int64     `json:"roundId"`
	AgentID   int64     `json:"agentId"`
	Status    string    `json:"status"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Text      string    `json:"text"`
	Basis     string    `json:"basis"`
	Options   []string  `json:"options"` // mereu non-nil
	Answer    string    `json:"answer"`
	Picked    bool      `json:"picked"`
	Priority  bool      `json:"priority"`
	FollowsID *int64    `json:"followsId"`
	RunID     string    `json:"runId"`
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type BrainstormFile struct {
	ID        int64     `json:"id"`
	TopicID   int64     `json:"topicId"`
	Name      string    `json:"name"`
	Stored    string    `json:"-"`
	Mime      string    `json:"mime"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

const brainstormCardCols = `id, topic_id, round_id, agent_id, status, kind, title, text, basis, options, answer, picked, priority,
	follows_id, run_id, error, created_at, updated_at`

func scanCard(sc scanner) (BrainstormCard, error) {
	var c BrainstormCard
	var opts, created, updated string
	err := sc.Scan(&c.ID, &c.TopicID, &c.RoundID, &c.AgentID, &c.Status, &c.Kind, &c.Title, &c.Text, &c.Basis, &opts, &c.Answer,
		&c.Picked, &c.Priority, &c.FollowsID, &c.RunID, &c.Error, &created, &updated)
	if err != nil {
		return BrainstormCard{}, notFound(err)
	}
	c.Options = parseList(opts)
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, nil
}

func (s *Store) cardsWhere(where string, args ...any) ([]BrainstormCard, error) {
	rows, err := s.db.Query(`SELECT `+brainstormCardCols+` FROM brainstorm_cards `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrainstormCard{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// setParticipants inlocuieste participantii subiectului (in tranzactia data).
func setParticipants(tx *sql.Tx, topicID int64, agents []int64) error {
	if _, err := tx.Exec(`DELETE FROM brainstorm_participants WHERE topic_id = ?`, topicID); err != nil {
		return err
	}
	for _, a := range agents {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO brainstorm_participants (topic_id, agent_id) VALUES (?, ?)`, topicID, a); err != nil {
			return err
		}
	}
	return nil
}

// CreateTopic salveaza subiectul; un nume luat in proiect da ErrConflict.
func (s *Store) CreateTopic(t *BrainstormTopic) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := now()
	res, err := tx.Exec(`INSERT INTO brainstorm_topics (project_id, name, description, archived, created_at) VALUES (?, ?, ?, ?, ?)`,
		t.ProjectID, t.Name, t.Description, t.Archived, ts)
	if isUnique(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if t.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	if err := setParticipants(tx, t.ID, t.Participants); err != nil {
		return err
	}
	if t.Participants == nil {
		t.Participants = []int64{}
	}
	t.CreatedAt = parseTime(ts)
	return tx.Commit()
}

// UpdateTopic schimba numele, descrierea, arhivarea si inlocuieste participantii.
func (s *Store) UpdateTopic(t BrainstormTopic) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE brainstorm_topics SET name = ?, description = ?, archived = ? WHERE id = ?`, t.Name, t.Description, t.Archived, t.ID)
	if isUnique(err) {
		return ErrConflict
	}
	if err := mustAffect(res, err); err != nil {
		return err
	}
	if err := setParticipants(tx, t.ID, t.Participants); err != nil {
		return err
	}
	return tx.Commit()
}

func scanTopic(sc scanner) (BrainstormTopic, error) {
	var t BrainstormTopic
	var created string
	if err := sc.Scan(&t.ID, &t.ProjectID, &t.Name, &t.Description, &t.Archived, &created); err != nil {
		return BrainstormTopic{}, notFound(err)
	}
	t.CreatedAt = parseTime(created)
	t.Participants = []int64{}
	return t, nil
}

const topicCols = `id, project_id, name, description, archived, created_at`

// fillParticipants completeaza Participants pentru toate subiectele date.
func (s *Store) fillParticipants(topics []BrainstormTopic) error {
	for i := range topics {
		rows, err := s.db.Query(`SELECT agent_id FROM brainstorm_participants WHERE topic_id = ? ORDER BY agent_id`, topics[i].ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			topics[i].Participants = append(topics[i].Participants, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) GetTopic(id int64) (BrainstormTopic, error) {
	t, err := scanTopic(s.db.QueryRow(`SELECT `+topicCols+` FROM brainstorm_topics WHERE id = ?`, id))
	if err != nil {
		return BrainstormTopic{}, err
	}
	l := []BrainstormTopic{t}
	if err := s.fillParticipants(l); err != nil {
		return BrainstormTopic{}, err
	}
	return l[0], nil
}

// ListTopics intoarce subiectele proiectului, in ordinea crearii.
func (s *Store) ListTopics(projectID int64) ([]BrainstormTopic, error) {
	rows, err := s.db.Query(`SELECT `+topicCols+` FROM brainstorm_topics WHERE project_id = ? ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	out := []BrainstormTopic{}
	for rows.Next() {
		t, err := scanTopic(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	rows.Close() // o singura conexiune: inchidem cursorul inainte de alte interogari
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.fillParticipants(out)
}

// AddRound salveaza runda si cate un card waiting per agent, intr-o tranzactie.
func (s *Store) AddRound(r *BrainstormRound, agentIDs []int64) ([]BrainstormCard, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ts := now()
	res, err := tx.Exec(`INSERT INTO brainstorm_rounds (topic_id, text, created_at) VALUES (?, ?, ?)`, r.TopicID, r.Text, ts)
	if err != nil {
		return nil, err
	}
	if r.ID, err = res.LastInsertId(); err != nil {
		return nil, err
	}
	r.CreatedAt = parseTime(ts)
	cards := []BrainstormCard{}
	for _, a := range agentIDs {
		res, err := tx.Exec(`INSERT INTO brainstorm_cards (topic_id, round_id, agent_id, status, created_at, updated_at)
			VALUES (?, ?, ?, 'waiting', ?, ?)`, r.TopicID, r.ID, a, ts, ts)
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		cards = append(cards, BrainstormCard{ID: id, TopicID: r.TopicID, RoundID: r.ID, AgentID: a, Status: CardWaiting,
			Options: []string{}, CreatedAt: parseTime(ts), UpdatedAt: parseTime(ts)})
	}
	return cards, tx.Commit()
}

// AddCard salveaza un card cu campurile date (folosit pentru raspunsuri la o intrebare, FollowsID).
func (s *Store) AddCard(c *BrainstormCard) error {
	if c.Status == "" {
		c.Status = CardWaiting
	}
	ts := now()
	res, err := s.db.Exec(`INSERT INTO brainstorm_cards (topic_id, round_id, agent_id, status, kind, title, text, basis, options, answer,
		picked, priority, follows_id, run_id, error, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.TopicID, c.RoundID, c.AgentID, c.Status, c.Kind, c.Title, c.Text, c.Basis, jsonList(c.Options), c.Answer,
		c.Picked, c.Priority, nullPtr(c.FollowsID), c.RunID, c.Error, ts, ts)
	if err != nil {
		return err
	}
	if c.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	if c.Options == nil {
		c.Options = []string{}
	}
	c.CreatedAt, c.UpdatedAt = parseTime(ts), parseTime(ts)
	return nil
}

func (s *Store) GetCard(id int64) (BrainstormCard, error) {
	return scanCard(s.db.QueryRow(`SELECT `+brainstormCardCols+` FROM brainstorm_cards WHERE id = ?`, id))
}

func (s *Store) TopicRounds(topicID int64) ([]BrainstormRound, error) {
	rows, err := s.db.Query(`SELECT id, topic_id, text, created_at FROM brainstorm_rounds WHERE topic_id = ? ORDER BY id`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrainstormRound{}
	for rows.Next() {
		var r BrainstormRound
		var created string
		if err := rows.Scan(&r.ID, &r.TopicID, &r.Text, &created); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) TopicCards(topicID int64) ([]BrainstormCard, error) {
	return s.cardsWhere(`WHERE topic_id = ? ORDER BY id`, topicID)
}

// NextCard intoarce cardul waiting al agentului: intai cele cu prioritate, apoi cel mai vechi.
// Cu priorityOnly doar cele cu prioritate.
func (s *Store) NextCard(agentID int64, priorityOnly bool) (BrainstormCard, bool, error) {
	q := `WHERE agent_id = ? AND status = 'waiting'`
	if priorityOnly {
		q += ` AND priority = 1`
	}
	cards, err := s.cardsWhere(q+` ORDER BY priority DESC, id LIMIT 1`, agentID)
	if err != nil || len(cards) == 0 {
		return BrainstormCard{}, false, err
	}
	return cards[0], true, nil
}

// SetCardState schimba starea cardului (cu run-ul si eroarea care o insotesc).
func (s *Store) SetCardState(id int64, status, runID, errText string) error {
	return mustAffect(s.db.Exec(`UPDATE brainstorm_cards SET status = ?, run_id = ?, error = ?, updated_at = ? WHERE id = ?`,
		status, runID, errText, now(), id))
}

// FinishCard completeaza cardul si il trece pe done; unul deja done da ErrConflict.
func (s *Store) FinishCard(id int64, kind, title, text, basis string, options []string) error {
	res, err := s.db.Exec(`UPDATE brainstorm_cards SET status = 'done', kind = ?, title = ?, text = ?, basis = ?, options = ?, error = '',
		updated_at = ? WHERE id = ? AND status <> 'done'`, kind, title, text, basis, jsonList(options), now(), id)
	if mustAffect(res, err) == ErrNotFound {
		if _, gerr := s.GetCard(id); gerr == nil {
			return ErrConflict
		}
		return ErrNotFound
	}
	return err
}

// AnswerCard salveaza raspunsul la o intrebare; doar un card question done fara raspuns.
func (s *Store) AnswerCard(id int64, answer string) error {
	res, err := s.db.Exec(`UPDATE brainstorm_cards SET answer = ?, updated_at = ?
		WHERE id = ? AND kind = 'question' AND status = 'done' AND answer = ''`, answer, now(), id)
	if mustAffect(res, err) == ErrNotFound {
		if _, gerr := s.GetCard(id); gerr == nil {
			return ErrConflict
		}
		return ErrNotFound
	}
	return err
}

func (s *Store) SetCardPicked(id int64, picked bool) error {
	return mustAffect(s.db.Exec(`UPDATE brainstorm_cards SET picked = ? WHERE id = ?`, picked, id))
}

func (s *Store) SetCardPriority(id int64, on bool) error {
	return mustAffect(s.db.Exec(`UPDATE brainstorm_cards SET priority = ? WHERE id = ?`, on, id))
}

// QuestionsAsked numara intrebarile puse de agent in runda (si cele fara raspuns).
func (s *Store) QuestionsAsked(roundID, agentID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM brainstorm_cards WHERE round_id = ? AND agent_id = ? AND kind = 'question' AND status = 'done'`,
		roundID, agentID).Scan(&n)
	return n, err
}

// OpenQuestions: pentru fiecare subiect al proiectului, intrebarile fara raspuns.
func (s *Store) OpenQuestions(projectID int64) (map[int64]int, error) {
	rows, err := s.db.Query(`SELECT c.topic_id, COUNT(*) FROM brainstorm_cards c JOIN brainstorm_topics t ON t.id = c.topic_id
		WHERE t.project_id = ? AND c.kind = 'question' AND c.status = 'done' AND c.answer = '' GROUP BY c.topic_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var tid int64
		var n int
		if err := rows.Scan(&tid, &n); err != nil {
			return nil, err
		}
		out[tid] = n
	}
	return out, rows.Err()
}

// SkipOrphanCards trece pe skipped cardurile waiting / stopped / failed ale agentilor care nu mai
// participa la subiect sau au fost stersi.
func (s *Store) SkipOrphanCards(projectID int64) (int64, error) {
	res, err := s.db.Exec(`UPDATE brainstorm_cards SET status = 'skipped', updated_at = ?
		WHERE status IN ('waiting', 'stopped', 'failed')
		AND topic_id IN (SELECT id FROM brainstorm_topics WHERE project_id = ?)
		AND (NOT EXISTS (SELECT 1 FROM brainstorm_participants p WHERE p.topic_id = brainstorm_cards.topic_id AND p.agent_id = brainstorm_cards.agent_id)
		  OR agent_id IN (SELECT id FROM agents WHERE deleted = 1))`, now(), projectID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RequeueRunningCards: la restart, cardurile running revin in waiting.
func (s *Store) RequeueRunningCards() error {
	_, err := s.db.Exec(`UPDATE brainstorm_cards SET status = 'waiting', run_id = '', updated_at = ? WHERE status = 'running'`, now())
	return err
}

// BrainstormSession intoarce sesiunea Claude a agentului pe subiect ("" daca nu exista).
func (s *Store) BrainstormSession(topicID, agentID int64) (sessionID, hash string, err error) {
	err = s.db.QueryRow(`SELECT session_id, prompt_hash FROM brainstorm_sessions WHERE topic_id = ? AND agent_id = ?`, topicID, agentID).
		Scan(&sessionID, &hash)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return sessionID, hash, err
}

func (s *Store) SetBrainstormSession(topicID, agentID int64, sessionID, hash string) error {
	_, err := s.db.Exec(`INSERT INTO brainstorm_sessions (topic_id, agent_id, session_id, prompt_hash) VALUES (?, ?, ?, ?)
		ON CONFLICT (topic_id, agent_id) DO UPDATE SET session_id = excluded.session_id, prompt_hash = excluded.prompt_hash`,
		topicID, agentID, sessionID, hash)
	return err
}

func (s *Store) AddTopicFile(f *BrainstormFile) error {
	ts := now()
	res, err := s.db.Exec(`INSERT INTO brainstorm_files (topic_id, name, stored, mime, size, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		f.TopicID, f.Name, f.Stored, f.Mime, f.Size, ts)
	if err != nil {
		return err
	}
	if f.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	f.CreatedAt = parseTime(ts)
	return nil
}

func scanTopicFile(sc scanner) (BrainstormFile, error) {
	var f BrainstormFile
	var created string
	if err := sc.Scan(&f.ID, &f.TopicID, &f.Name, &f.Stored, &f.Mime, &f.Size, &created); err != nil {
		return BrainstormFile{}, notFound(err)
	}
	f.CreatedAt = parseTime(created)
	return f, nil
}

const topicFileCols = `id, topic_id, name, stored, mime, size, created_at`

func (s *Store) ListTopicFiles(topicID int64) ([]BrainstormFile, error) {
	rows, err := s.db.Query(`SELECT `+topicFileCols+` FROM brainstorm_files WHERE topic_id = ? ORDER BY id`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrainstormFile{}
	for rows.Next() {
		f, err := scanTopicFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) GetTopicFile(id int64) (BrainstormFile, error) {
	return scanTopicFile(s.db.QueryRow(`SELECT `+topicFileCols+` FROM brainstorm_files WHERE id = ?`, id))
}

func (s *Store) DeleteTopicFile(id int64) error {
	return mustAffect(s.db.Exec(`DELETE FROM brainstorm_files WHERE id = ?`, id))
}

// FindProjectFile gaseste fisierul de proiect cu numele dat (cel mai nou); ErrNotFound daca nu exista.
func (s *Store) FindProjectFile(projectID int64, name string) (Attachment, error) {
	a, err := scanAttachment(s.db.QueryRow(`SELECT `+attachmentCols+` FROM attachments
		WHERE project_id = ? AND kind = ? AND name = ? ORDER BY id DESC LIMIT 1`, projectID, KindProjectFile, name))
	if err == sql.ErrNoRows {
		return Attachment{}, ErrNotFound
	}
	return a, err
}

// SetAttachmentSize actualizeaza marimea dupa ce fisierul de pe disc a fost suprascris.
func (s *Store) SetAttachmentSize(id, size int64) error {
	return mustAffect(s.db.Exec(`UPDATE attachments SET size = ? WHERE id = ?`, size, id))
}
