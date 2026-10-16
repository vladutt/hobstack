-- agentul care a creat sarcina la cererea utilizatorului (NULL = utilizatorul)
ALTER TABLE recurring_tasks ADD COLUMN created_by INTEGER REFERENCES agents(id) ON DELETE SET NULL;
