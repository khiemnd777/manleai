CREATE TABLE pos_provider_switch_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    from_provider TEXT NOT NULL,
    to_provider TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'blocked', 'importing', 'matching', 'needs_review', 'ready', 'activated', 'cancelled', 'failed')),
    blocked_reason TEXT,
    dry_run_ready BOOLEAN NOT NULL DEFAULT false,
    activated_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_pos_provider_switch_runs_salon_created
    ON pos_provider_switch_runs(salon_id, created_at DESC);

CREATE INDEX idx_pos_provider_switch_runs_status
    ON pos_provider_switch_runs(salon_id, status, created_at DESC);

CREATE TABLE pos_provider_switch_matches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES pos_provider_switch_runs(id) ON DELETE CASCADE,
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL CHECK (entity_type IN ('service', 'staff', 'customer')),
    canonical_entity_id UUID,
    canonical_name TEXT,
    provider_entity_id TEXT NOT NULL,
    provider_name TEXT NOT NULL,
    provider_phone TEXT,
    provider_email TEXT,
    provider_duration_minutes INTEGER,
    match_status TEXT NOT NULL DEFAULT 'unmatched' CHECK (match_status IN ('suggested', 'unmatched', 'conflict', 'confirmed', 'skipped')),
    match_confidence INTEGER NOT NULL DEFAULT 0 CHECK (match_confidence BETWEEN 0 AND 100),
    match_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (run_id, entity_type, provider_entity_id)
);

CREATE INDEX idx_pos_provider_switch_matches_run
    ON pos_provider_switch_matches(run_id, entity_type, match_status);

CREATE INDEX idx_pos_provider_switch_matches_salon
    ON pos_provider_switch_matches(salon_id, entity_type, canonical_entity_id);
