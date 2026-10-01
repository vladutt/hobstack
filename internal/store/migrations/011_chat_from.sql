-- "New chat": thread-ul agentului se arata de la mesajul chat_from incolo
-- (0 = tot istoricul). Mesajele mai vechi raman salvate.
ALTER TABLE agents ADD COLUMN chat_from INTEGER NOT NULL DEFAULT 0;
