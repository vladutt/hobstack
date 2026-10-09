-- Categoria proiectului (ex. Work, Personal), pentru filtrele si gruparea din lista de proiecte; '' = fara categorie
ALTER TABLE projects ADD COLUMN category TEXT NOT NULL DEFAULT '';
