package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ErrBadAnswer: raspunsul nu se potriveste cu intrebarea (alegere necunoscuta, prea multe alegeri, deja raspunsa).
var ErrBadAnswer = errors.New("invalid answer")

// Decision e o intrebare cu optiuni pusa de un agent la report_done.
type Decision struct {
	ID         int64      `json:"id"`
	TaskID     int64      `json:"taskId"`
	Position   int        `json:"position"`
	Question   string     `json:"question"`
	Options    []string   `json:"options"`
	Multi      bool       `json:"multi"`
	AskedBy    string     `json:"askedBy"`
	AskedAt    time.Time  `json:"askedAt"`
	Answer     []string   `json:"answer"`
	Other      string     `json:"other"`
	AnsweredAt *time.Time `json:"answeredAt"`
}

type DecisionAnswer struct {
	ID      int64
	Choices []string
	Other   string
}

// SetDecisions sterge intrebarile fara raspuns ale lui askedBy si adauga intrebarile noi la coada.
func (s *Store) SetDecisions(taskID int64, askedBy string, qs []Decision) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM task_decisions WHERE task_id = ? AND asked_by = ? AND answered_at IS NULL`, taskID, askedBy); err != nil {
		return err
	}
	var last int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(position), 0) FROM task_decisions WHERE task_id = ?`, taskID).Scan(&last); err != nil {
		return err
	}
	for _, q := range qs {
		last++
		opts, _ := json.Marshal(nonNil(q.Options))
		if _, err := tx.Exec(`INSERT INTO task_decisions (task_id, position, question, options, multi, asked_by, asked_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, taskID, last, q.Question, string(opts), q.Multi, askedBy, now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// ListDecisions intoarce toate intrebarile task-ului, in ordinea pozitiei (niciodata nil).
func (s *Store) ListDecisions(taskID int64) ([]Decision, error) {
	rows, err := s.db.Query(`SELECT id, task_id, position, question, options, multi, asked_by, asked_at, answer, other, answered_at
		FROM task_decisions WHERE task_id = ? ORDER BY position`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Decision{}
	for rows.Next() {
		var d Decision
		var opts, ans, asked string
		var answered sql.NullString
		if err := rows.Scan(&d.ID, &d.TaskID, &d.Position, &d.Question, &opts, &d.Multi, &d.AskedBy, &asked, &ans, &d.Other, &answered); err != nil {
			return nil, err
		}
		d.AskedAt = parseTime(asked)
		d.Options, d.Answer = []string{}, []string{}
		json.Unmarshal([]byte(opts), &d.Options)
		json.Unmarshal([]byte(ans), &d.Answer)
		if answered.Valid {
			at := parseTime(answered.String)
			d.AnsweredAt = &at
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AnswerDecisions valideaza toate raspunsurile si le salveaza intr-o tranzactie; la prima eroare nu se salveaza nimic.
func (s *Store) AnswerDecisions(taskID int64, answers []DecisionAnswer) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, a := range answers {
		var optsJSON, ansJSON, other string
		var multi bool
		var answered sql.NullString
		err := tx.QueryRow(`SELECT options, multi, answer, other, answered_at FROM task_decisions WHERE id = ? AND task_id = ?`, a.ID, taskID).
			Scan(&optsJSON, &multi, &ansJSON, &other, &answered)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: question %d does not belong to this task", ErrBadAnswer, a.ID)
		}
		if err != nil {
			return err
		}
		if answered.Valid {
			// reincercare cu exact acelasi raspuns (ex. decizia a picat dupa salvare): nimic de facut
			var prev []string
			json.Unmarshal([]byte(ansJSON), &prev)
			if sameChoices(prev, a.Choices) && strings.TrimSpace(other) == strings.TrimSpace(a.Other) {
				continue
			}
			return fmt.Errorf("%w: question %d is already answered", ErrBadAnswer, a.ID)
		}
		var opts []string
		json.Unmarshal([]byte(optsJSON), &opts)
		if !multi && len(a.Choices) > 1 {
			return fmt.Errorf("%w: question %d takes a single choice", ErrBadAnswer, a.ID)
		}
		for _, c := range a.Choices {
			if !slices.Contains(opts, c) {
				return fmt.Errorf("%w: %q is not an option of question %d", ErrBadAnswer, c, a.ID)
			}
		}
		ans, _ := json.Marshal(nonNil(a.Choices))
		if _, err := tx.Exec(`UPDATE task_decisions SET answer = ?, other = ?, answered_at = ? WHERE id = ?`,
			string(ans), a.Other, now(), a.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// sameChoices: aceeasi multime de alegeri, indiferent de ordine.
func sameChoices(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
