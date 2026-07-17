CREATE TABLE service_taxonomy_releases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    release_key TEXT NOT NULL UNIQUE,
    locale TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version > 0),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'retired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX service_taxonomy_one_active_locale
    ON service_taxonomy_releases(locale)
    WHERE status = 'active';

CREATE TABLE service_taxonomy_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id UUID NOT NULL REFERENCES service_taxonomy_releases(id) ON DELETE CASCADE,
    category_key TEXT NOT NULL,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT,
    sort_order INTEGER NOT NULL DEFAULT 0,
    confidence NUMERIC(4,3) NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'retired')),
    UNIQUE (release_id, category_key),
    UNIQUE (release_id, slug)
);

CREATE TABLE service_taxonomy_category_aliases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id UUID NOT NULL REFERENCES service_taxonomy_categories(id) ON DELETE CASCADE,
    alias TEXT NOT NULL,
    normalized_alias TEXT NOT NULL,
    confidence NUMERIC(4,3) NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'retired')),
    UNIQUE (category_id, normalized_alias)
);

CREATE TABLE service_taxonomy_service_concepts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id UUID NOT NULL REFERENCES service_taxonomy_releases(id) ON DELETE CASCADE,
    category_id UUID NOT NULL REFERENCES service_taxonomy_categories(id) ON DELETE RESTRICT,
    concept_key TEXT NOT NULL,
    canonical_name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    confidence NUMERIC(4,3) NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'retired')),
    UNIQUE (release_id, concept_key),
    UNIQUE (release_id, normalized_name)
);

CREATE TABLE service_taxonomy_service_aliases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    concept_id UUID NOT NULL REFERENCES service_taxonomy_service_concepts(id) ON DELETE CASCADE,
    alias TEXT NOT NULL,
    normalized_alias TEXT NOT NULL,
    confidence NUMERIC(4,3) NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'retired')),
    UNIQUE (concept_id, normalized_alias)
);

CREATE INDEX idx_service_taxonomy_categories_release
    ON service_taxonomy_categories(release_id, status, sort_order);

CREATE INDEX idx_service_taxonomy_concepts_release_name
    ON service_taxonomy_service_concepts(release_id, status, normalized_name);

ALTER TABLE service_aliases
    DROP CONSTRAINT IF EXISTS service_aliases_source_check;

ALTER TABLE service_aliases
    ADD CONSTRAINT service_aliases_source_check
    CHECK (source IN ('owner', 'correction', 'import', 'system'));

INSERT INTO service_taxonomy_releases (release_key, locale, version, status, activated_at)
VALUES ('us-nail-v1', 'en-US', 1, 'active', now())
ON CONFLICT (release_key) DO UPDATE
SET locale = EXCLUDED.locale,
    version = EXCLUDED.version,
    status = EXCLUDED.status,
    activated_at = COALESCE(service_taxonomy_releases.activated_at, EXCLUDED.activated_at);

WITH release AS (
    SELECT id FROM service_taxonomy_releases WHERE release_key = 'us-nail-v1'
), category_data(category_key, name, slug, description, sort_order, confidence) AS (
    VALUES
        ('manicure', 'Manicure', 'manicure', 'Hand nail grooming and polish services.', 10, 0.960),
        ('pedicure', 'Pedicure', 'pedicure', 'Foot nail grooming and polish services.', 20, 0.960),
        ('acrylic', 'Acrylic', 'acrylic', 'Acrylic enhancement sets, fills, and overlays.', 30, 0.950),
        ('dip-powder', 'Dip Powder', 'dip-powder', 'Dip powder and SNS-style nail services.', 40, 0.950),
        ('removal', 'Removal', 'removal', 'Removal of gel, acrylic, dip, or other enhancements.', 50, 0.940),
        ('nail-art', 'Nail Art', 'nail-art', 'Decorative art, designs, and embellishments.', 60, 0.940),
        ('repair', 'Nail Repair', 'nail-repair', 'Repair of cracked, chipped, or broken nails.', 70, 0.940),
        ('polish-change', 'Polish Change', 'polish-change', 'Polish or color change without a full manicure or pedicure.', 80, 0.930),
        ('extensions', 'Nail Extensions', 'nail-extensions', 'Tip and extension services that add nail length.', 90, 0.930),
        ('kids', 'Kids Nails', 'kids-nails', 'Age-appropriate manicure and pedicure services for children.', 100, 0.920)
)
INSERT INTO service_taxonomy_categories (
    release_id, category_key, name, slug, description, sort_order, confidence, status
)
SELECT release.id, data.category_key, data.name, data.slug, data.description, data.sort_order, data.confidence, 'active'
FROM release
CROSS JOIN category_data data
ON CONFLICT (release_id, category_key) DO UPDATE
SET name = EXCLUDED.name,
    slug = EXCLUDED.slug,
    description = EXCLUDED.description,
    sort_order = EXCLUDED.sort_order,
    confidence = EXCLUDED.confidence,
    status = 'active';

WITH release AS (
    SELECT id FROM service_taxonomy_releases WHERE release_key = 'us-nail-v1'
), alias_data(category_key, alias, normalized_alias, confidence) AS (
    VALUES
        ('manicure', 'mani', 'mani', 0.930),
        ('manicure', 'hand nails', 'hand nails', 0.900),
        ('pedicure', 'pedi', 'pedi', 0.930),
        ('pedicure', 'foot nails', 'foot nails', 0.900),
        ('acrylic', 'acrylics', 'acrylics', 0.920),
        ('acrylic', 'acrylic enhancements', 'acrylic enhancements', 0.900),
        ('dip-powder', 'dip nails', 'dip nails', 0.920),
        ('dip-powder', 'SNS', 'sns', 0.920),
        ('removal', 'take off', 'take off', 0.900),
        ('removal', 'soak off', 'soak off', 0.900),
        ('nail-art', 'nail designs', 'nail designs', 0.910),
        ('nail-art', 'designs', 'designs', 0.880),
        ('repair', 'fix a nail', 'fix a nail', 0.900),
        ('repair', 'broken nail', 'broken nail', 0.900),
        ('polish-change', 'color change', 'color change', 0.900),
        ('polish-change', 'polish only', 'polish only', 0.880),
        ('extensions', 'extensions', 'extensions', 0.900),
        ('extensions', 'nail tips', 'nail tips', 0.890),
        ('kids', 'kids nails', 'kids nails', 0.900),
        ('kids', 'child nails', 'child nails', 0.880)
)
INSERT INTO service_taxonomy_category_aliases (category_id, alias, normalized_alias, confidence, status)
SELECT category.id, data.alias, data.normalized_alias, data.confidence, 'active'
FROM alias_data data
JOIN release ON true
JOIN service_taxonomy_categories category
  ON category.release_id = release.id
 AND category.category_key = data.category_key
ON CONFLICT (category_id, normalized_alias) DO UPDATE
SET alias = EXCLUDED.alias,
    confidence = EXCLUDED.confidence,
    status = 'active';

WITH release AS (
    SELECT id FROM service_taxonomy_releases WHERE release_key = 'us-nail-v1'
), concept_data(concept_key, category_key, canonical_name, normalized_name, confidence) AS (
    VALUES
        ('classic-manicure', 'manicure', 'Classic Manicure', 'classic manicure', 0.980),
        ('gel-manicure', 'manicure', 'Gel Manicure', 'gel manicure', 0.980),
        ('spa-manicure', 'manicure', 'Spa Manicure', 'spa manicure', 0.960),
        ('classic-pedicure', 'pedicure', 'Classic Pedicure', 'classic pedicure', 0.980),
        ('gel-pedicure', 'pedicure', 'Gel Pedicure', 'gel pedicure', 0.980),
        ('spa-pedicure', 'pedicure', 'Spa Pedicure', 'spa pedicure', 0.970),
        ('acrylic-full-set', 'acrylic', 'Acrylic Full Set', 'acrylic full set', 0.980),
        ('acrylic-fill', 'acrylic', 'Acrylic Fill', 'acrylic fill', 0.970),
        ('acrylic-overlay', 'acrylic', 'Acrylic Overlay', 'acrylic overlay', 0.960),
        ('dip-powder-manicure', 'dip-powder', 'Dip Powder Manicure', 'dip powder manicure', 0.980),
        ('gel-removal', 'removal', 'Gel Removal', 'gel removal', 0.970),
        ('acrylic-removal', 'removal', 'Acrylic Removal', 'acrylic removal', 0.970),
        ('dip-removal', 'removal', 'Dip Removal', 'dip removal', 0.960),
        ('nail-art', 'nail-art', 'Nail Art', 'nail art', 0.970),
        ('nail-repair', 'repair', 'Nail Repair', 'nail repair', 0.970),
        ('polish-change', 'polish-change', 'Polish Change', 'polish change', 0.960),
        ('nail-extensions', 'extensions', 'Nail Extensions', 'nail extensions', 0.950),
        ('kids-manicure', 'kids', 'Kids Manicure', 'kids manicure', 0.950),
        ('kids-pedicure', 'kids', 'Kids Pedicure', 'kids pedicure', 0.950)
)
INSERT INTO service_taxonomy_service_concepts (
    release_id, category_id, concept_key, canonical_name, normalized_name, confidence, status
)
SELECT release.id, category.id, data.concept_key, data.canonical_name, data.normalized_name, data.confidence, 'active'
FROM concept_data data
JOIN release ON true
JOIN service_taxonomy_categories category
  ON category.release_id = release.id
 AND category.category_key = data.category_key
ON CONFLICT (release_id, concept_key) DO UPDATE
SET category_id = EXCLUDED.category_id,
    canonical_name = EXCLUDED.canonical_name,
    normalized_name = EXCLUDED.normalized_name,
    confidence = EXCLUDED.confidence,
    status = 'active';

WITH release AS (
    SELECT id FROM service_taxonomy_releases WHERE release_key = 'us-nail-v1'
), alias_data(concept_key, alias, normalized_alias, confidence) AS (
    VALUES
        ('classic-manicure', 'regular manicure', 'regular manicure', 0.950),
        ('classic-manicure', 'basic manicure', 'basic manicure', 0.940),
        ('classic-manicure', 'classic mani', 'classic mani', 0.950),
        ('gel-manicure', 'gel mani', 'gel mani', 0.960),
        ('gel-manicure', 'shellac manicure', 'shellac manicure', 0.940),
        ('gel-manicure', 'no chip manicure', 'no chip manicure', 0.930),
        ('spa-manicure', 'deluxe manicure', 'deluxe manicure', 0.930),
        ('spa-manicure', 'spa mani', 'spa mani', 0.940),
        ('classic-pedicure', 'regular pedicure', 'regular pedicure', 0.950),
        ('classic-pedicure', 'basic pedicure', 'basic pedicure', 0.940),
        ('classic-pedicure', 'classic pedi', 'classic pedi', 0.950),
        ('gel-pedicure', 'gel pedi', 'gel pedi', 0.960),
        ('gel-pedicure', 'shellac pedicure', 'shellac pedicure', 0.940),
        ('spa-pedicure', 'deluxe pedicure', 'deluxe pedicure', 0.940),
        ('spa-pedicure', 'spa pedi', 'spa pedi', 0.950),
        ('acrylic-full-set', 'acrylic set', 'acrylic set', 0.950),
        ('acrylic-full-set', 'full set acrylic', 'full set acrylic', 0.940),
        ('acrylic-fill', 'acrylic refill', 'acrylic refill', 0.950),
        ('acrylic-fill', 'acrylic fill in', 'acrylic fill in', 0.940),
        ('acrylic-overlay', 'overlay acrylic', 'overlay acrylic', 0.920),
        ('dip-powder-manicure', 'dip manicure', 'dip manicure', 0.960),
        ('dip-powder-manicure', 'SNS manicure', 'sns manicure', 0.950),
        ('gel-removal', 'gel take off', 'gel take off', 0.950),
        ('gel-removal', 'gel soak off', 'gel soak off', 0.950),
        ('acrylic-removal', 'acrylic take off', 'acrylic take off', 0.950),
        ('acrylic-removal', 'acrylic soak off', 'acrylic soak off', 0.940),
        ('dip-removal', 'dip take off', 'dip take off', 0.940),
        ('dip-removal', 'SNS removal', 'sns removal', 0.940),
        ('nail-art', 'custom nail art', 'custom nail art', 0.940),
        ('nail-art', 'nail design service', 'nail design service', 0.920),
        ('nail-repair', 'broken nail repair', 'broken nail repair', 0.950),
        ('polish-change', 'polish only service', 'polish only service', 0.900),
        ('nail-extensions', 'extension set', 'extension set', 0.920),
        ('nail-extensions', 'tips full set', 'tips full set', 0.920),
        ('kids-manicure', 'child manicure', 'child manicure', 0.920),
        ('kids-pedicure', 'child pedicure', 'child pedicure', 0.920)
)
INSERT INTO service_taxonomy_service_aliases (concept_id, alias, normalized_alias, confidence, status)
SELECT concept.id, data.alias, data.normalized_alias, data.confidence, 'active'
FROM alias_data data
JOIN release ON true
JOIN service_taxonomy_service_concepts concept
  ON concept.release_id = release.id
 AND concept.concept_key = data.concept_key
ON CONFLICT (concept_id, normalized_alias) DO UPDATE
SET alias = EXCLUDED.alias,
    confidence = EXCLUDED.confidence,
    status = 'active';

-- Materialize system categories without replacing an owner/imported category
-- that already owns the same salon slug.
INSERT INTO service_categories (
    salon_id, name, slug, description, status, sort_order, source
)
SELECT salon.id, taxonomy.name, taxonomy.slug, taxonomy.description, 'active', taxonomy.sort_order, 'system'
FROM salons salon
JOIN service_taxonomy_releases release ON release.locale = 'en-US' AND release.status = 'active'
JOIN service_taxonomy_categories taxonomy ON taxonomy.release_id = release.id AND taxonomy.status = 'active'
ON CONFLICT (salon_id, slug) DO UPDATE
SET name = EXCLUDED.name,
    description = EXCLUDED.description,
    status = 'active',
    sort_order = EXCLUDED.sort_order,
    archived_at = NULL,
    updated_at = now()
WHERE service_categories.source = 'system';

INSERT INTO service_category_aliases (
    salon_id, category_id, alias, normalized_alias, source, status, confidence
)
SELECT category.salon_id, category.id, taxonomy_alias.alias, taxonomy_alias.normalized_alias,
       'system', 'active', taxonomy_alias.confidence
FROM service_taxonomy_releases release
JOIN service_taxonomy_categories taxonomy
  ON taxonomy.release_id = release.id AND taxonomy.status = 'active'
JOIN service_taxonomy_category_aliases taxonomy_alias
  ON taxonomy_alias.category_id = taxonomy.id AND taxonomy_alias.status = 'active'
JOIN service_categories category
  ON category.slug = taxonomy.slug AND category.source = 'system' AND category.status = 'active'
WHERE release.locale = 'en-US'
  AND release.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM service_aliases service_alias
      WHERE service_alias.salon_id = category.salon_id
        AND service_alias.normalized_alias = taxonomy_alias.normalized_alias
        AND service_alias.status = 'active'
  )
ON CONFLICT (salon_id, normalized_alias) DO UPDATE
SET category_id = EXCLUDED.category_id,
    alias = EXCLUDED.alias,
    status = 'active',
    confidence = EXCLUDED.confidence,
    updated_at = now()
WHERE service_category_aliases.source = 'system';

WITH exact_matches AS (
    SELECT service.id AS service_id,
           service.salon_id,
           category.id AS salon_category_id,
           concept.confidence
    FROM services service
    JOIN service_taxonomy_releases release
      ON release.locale = 'en-US' AND release.status = 'active'
    JOIN service_taxonomy_service_concepts concept
      ON concept.release_id = release.id
     AND concept.status = 'active'
     AND concept.normalized_name = lower(trim(regexp_replace(service.name, '[^a-zA-Z0-9]+', ' ', 'g')))
    JOIN service_taxonomy_categories taxonomy_category
      ON taxonomy_category.id = concept.category_id AND taxonomy_category.status = 'active'
    JOIN service_categories category
      ON category.salon_id = service.salon_id
     AND category.slug = taxonomy_category.slug
     AND category.source = 'system'
     AND category.status = 'active'
    WHERE service.archived_at IS NULL
)
UPDATE services service
SET service_category_id = match.salon_category_id,
    service_category_source = 'suggested',
    service_category_confidence = match.confidence,
    service_category_reviewed_by = NULL,
    service_category_reviewed_at = NULL,
    updated_at = now()
FROM exact_matches match
WHERE service.id = match.service_id
  AND service.salon_id = match.salon_id
  AND (service.service_category_source IN ('unassigned', 'suggested') OR service.service_category_id IS NULL);

WITH exact_matches AS (
    SELECT service.salon_id, service.id AS service_id, concept.id AS concept_id
    FROM services service
    JOIN service_taxonomy_releases release
      ON release.locale = 'en-US' AND release.status = 'active'
    JOIN service_taxonomy_service_concepts concept
      ON concept.release_id = release.id
     AND concept.status = 'active'
     AND concept.normalized_name = lower(trim(regexp_replace(service.name, '[^a-zA-Z0-9]+', ' ', 'g')))
    WHERE service.archived_at IS NULL
), alias_candidates AS (
    SELECT match.salon_id, match.service_id, alias.alias, alias.normalized_alias, alias.confidence
    FROM exact_matches match
    JOIN service_taxonomy_service_aliases alias
      ON alias.concept_id = match.concept_id AND alias.status = 'active'
), unique_targets AS (
    SELECT salon_id, normalized_alias
    FROM alias_candidates
    GROUP BY salon_id, normalized_alias
    HAVING COUNT(DISTINCT service_id) = 1
)
INSERT INTO service_aliases (
    salon_id, service_id, alias, normalized_alias, source, status, confidence
)
SELECT candidate.salon_id, (array_agg(candidate.service_id))[1], min(candidate.alias), candidate.normalized_alias,
       'system', 'active', max(candidate.confidence)
FROM alias_candidates candidate
JOIN unique_targets target
  ON target.salon_id = candidate.salon_id
 AND target.normalized_alias = candidate.normalized_alias
WHERE NOT EXISTS (
    SELECT 1
    FROM service_category_aliases category_alias
    WHERE category_alias.salon_id = candidate.salon_id
      AND category_alias.normalized_alias = candidate.normalized_alias
      AND category_alias.status = 'active'
)
GROUP BY candidate.salon_id, candidate.normalized_alias
ON CONFLICT (salon_id, normalized_alias) DO UPDATE
SET service_id = EXCLUDED.service_id,
    alias = EXCLUDED.alias,
    status = 'active',
    confidence = EXCLUDED.confidence,
    updated_at = now()
WHERE service_aliases.source = 'system';
