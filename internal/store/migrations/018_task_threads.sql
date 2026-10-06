-- fir de lucru: task-ul care a pornit munca (follow-up-urile create de lider cu
-- continues). Doar pentru afisare pe board; nicio decizie nu il citeste.
ALTER TABLE tasks ADD COLUMN thread_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL;
