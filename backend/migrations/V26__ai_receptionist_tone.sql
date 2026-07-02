ALTER TABLE salon_settings
    ADD COLUMN ai_tone TEXT NOT NULL DEFAULT 'professional_warm'
        CHECK (ai_tone IN ('professional_warm', 'natural_human', 'friendly_young', 'concise_calm'));
