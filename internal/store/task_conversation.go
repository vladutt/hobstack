package store

// ListTaskRunMessages intoarce mesajele agentilor (kind msg, nu ale utilizatorului) din
// run-urile unui task, cronologic; mereu non-nil.
func (s *Store) ListTaskRunMessages(taskID int64) ([]Message, error) {
	return s.queryMessages(`kind = ? AND author != ? AND run_id IN (SELECT id FROM runs WHERE task_id = ?)`,
		KindMsg, AuthorUser, taskID)
}
