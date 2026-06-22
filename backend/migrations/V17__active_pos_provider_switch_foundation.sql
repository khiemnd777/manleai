ALTER TABLE salons
    ADD COLUMN active_pos_provider TEXT NOT NULL DEFAULT 'square' CHECK (length(trim(active_pos_provider)) > 0);

CREATE INDEX idx_salons_active_pos_provider
    ON salons(active_pos_provider);
