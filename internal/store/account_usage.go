package store

import (
	"database/sql"
	"time"
)

// AccountUsage: cat din limitele unui cont Claude e folosit (0..1) si cand se reseteaza ferestrele.
// AccountID 0 = Default. Se actualizeaza la finalul fiecarui run care a raportat limitele.
type AccountUsage struct {
	AccountID      int64     `json:"accountId"`
	Status         string    `json:"status"` // allowed, allowed_warning, rejected
	FiveHourUsed   float64   `json:"fiveHourUsed"`
	FiveHourResets time.Time `json:"fiveHourResets"`
	SevenDayUsed   float64   `json:"sevenDayUsed"`
	SevenDayResets time.Time `json:"sevenDayResets"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// SaveAccountUsage inlocuieste randul contului.
func (s *Store) SaveAccountUsage(u AccountUsage) error {
	_, err := s.db.Exec(`INSERT INTO account_usage (account_id, status, five_hour_used, five_hour_resets, seven_day_used, seven_day_resets, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id) DO UPDATE SET status = excluded.status, five_hour_used = excluded.five_hour_used,
			five_hour_resets = excluded.five_hour_resets, seven_day_used = excluded.seven_day_used,
			seven_day_resets = excluded.seven_day_resets, updated_at = excluded.updated_at`,
		u.AccountID, u.Status, u.FiveHourUsed, timeOrNull(u.FiveHourResets), u.SevenDayUsed, timeOrNull(u.SevenDayResets), now())
	return err
}

// GetAccountUsage: ultimele limite stiute ale contului; nil daca niciun run nu le-a raportat inca.
func (s *Store) GetAccountUsage(accountID int64) (*AccountUsage, error) {
	u := AccountUsage{AccountID: accountID}
	var r5, r7 sql.NullString
	var updated string
	err := s.db.QueryRow(`SELECT status, five_hour_used, five_hour_resets, seven_day_used, seven_day_resets, updated_at
		FROM account_usage WHERE account_id = ?`, accountID).Scan(&u.Status, &u.FiveHourUsed, &r5, &u.SevenDayUsed, &r7, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if r5.Valid {
		u.FiveHourResets = parseTime(r5.String)
	}
	if r7.Valid {
		u.SevenDayResets = parseTime(r7.String)
	}
	u.UpdatedAt = parseTime(updated)
	return &u, nil
}

// timeOrNull: timpul zero se salveaza NULL (necunoscut).
func timeOrNull(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}
