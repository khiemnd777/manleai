ALTER TABLE salons
    ADD COLUMN public_slug TEXT,
    ADD COLUMN public_catalog_enabled BOOLEAN NOT NULL DEFAULT false;

CREATE UNIQUE INDEX idx_salons_public_slug_unique
    ON salons (lower(public_slug))
    WHERE public_slug IS NOT NULL;

CREATE INDEX idx_salons_public_catalog_enabled
    ON salons(public_catalog_enabled)
    WHERE public_catalog_enabled = true;
