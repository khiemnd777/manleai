CREATE TABLE salon_business_hour_periods (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    day_of_week SMALLINT NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
    start_local_time TIME NOT NULL,
    end_local_time TIME NOT NULL,
    source TEXT NOT NULL DEFAULT 'imported' CHECK (source IN ('imported', 'local_migrated', 'local_override')),
    provider TEXT NOT NULL DEFAULT '',
    provider_location_id TEXT NOT NULL DEFAULT '',
    provider_period_index INTEGER NOT NULL DEFAULT 0,
    last_synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_local_time > start_local_time),
    UNIQUE (salon_id, provider, provider_location_id, day_of_week, provider_period_index)
);

CREATE INDEX idx_salon_business_hour_periods_salon_day
    ON salon_business_hour_periods(salon_id, day_of_week, start_local_time);

CREATE INDEX idx_salon_business_hour_periods_provider
    ON salon_business_hour_periods(salon_id, provider, provider_location_id);

INSERT INTO salon_business_hour_periods (
    salon_id,
    day_of_week,
    start_local_time,
    end_local_time,
    source,
    provider,
    provider_location_id,
    provider_period_index,
    created_at,
    updated_at
)
SELECT
    salon_id,
    day_of_week,
    open_time,
    close_time,
    'local_migrated',
    '',
    '',
    0,
    created_at,
    updated_at
FROM salon_business_hours
WHERE is_closed = false
  AND open_time IS NOT NULL
  AND close_time IS NOT NULL
  AND close_time > open_time
ON CONFLICT DO NOTHING;
