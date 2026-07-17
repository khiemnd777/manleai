-- V44 intentionally preserved owner/imported categories with colliding slugs,
-- but its initial bulk materialization only attached taxonomy aliases and exact
-- service suggestions to system-owned categories. Forward-fill those records
-- against whichever active salon category owns the taxonomy slug. Owner/import
-- aliases and reviewed service assignments remain authoritative.

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
  ON category.slug = taxonomy.slug AND category.status = 'active'
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
