-- raspunsurile la intrebarile ask_user (raspuns sau dismiss): mesaje ale utilizatorului
-- care nu conteaza ca cerere din chat (nu dau drept de add_agent, nici pasul "requested")
ALTER TABLE messages ADD COLUMN reply INTEGER NOT NULL DEFAULT 0;
