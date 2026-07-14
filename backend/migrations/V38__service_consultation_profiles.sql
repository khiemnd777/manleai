ALTER TABLE salon_settings
    ADD COLUMN consultation_enabled BOOLEAN NOT NULL DEFAULT true;

ALTER TABLE services
    ADD CONSTRAINT uq_services_salon_id_id UNIQUE (salon_id, id);

CREATE TABLE service_consultation_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL,
    service_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'ready', 'disabled')),
    recommended_outcomes JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(recommended_outcomes) = 'array'),
    compatible_current_systems JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(compatible_current_systems) = 'array'),
    length_capabilities JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(length_capabilities) = 'array'),
    priority_tags JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(priority_tags) = 'array'),
    finish_options JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(finish_options) = 'array'),
    maintenance_note TEXT,
    owner_approved_summary TEXT,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fk_service_consultation_profile_service
        FOREIGN KEY (salon_id, service_id) REFERENCES services(salon_id, id) ON DELETE CASCADE,
    CONSTRAINT uq_service_consultation_profile_service UNIQUE (salon_id, service_id)
);

CREATE INDEX idx_service_consultation_profiles_salon_status
    ON service_consultation_profiles (salon_id, status);

UPDATE call_sessions
SET dialog_state = jsonb_set(
    COALESCE(dialog_state, '{}'::jsonb),
    '{version}',
    '3'::jsonb,
    true
);

ALTER TABLE call_sessions
    ALTER COLUMN dialog_state SET DEFAULT '{"version":3,"phase":"open","review_required":true,"review_accepted":false,"no_progress_count":0,"draft_revision":1,"reviewed_revision":0,"authorized_revision":0}'::jsonb;
