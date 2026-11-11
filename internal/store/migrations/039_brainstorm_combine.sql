-- Carduri combinate: sursele raman ca istoric (fara FK, cardurile nu se sterg individual).
ALTER TABLE brainstorm_cards ADD COLUMN source_a INTEGER;
ALTER TABLE brainstorm_cards ADD COLUMN source_b INTEGER;
ALTER TABLE brainstorm_cards ADD COLUMN edited INTEGER NOT NULL DEFAULT 0;
