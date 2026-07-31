-- Pre-tenant marketing registration, Platform review, atomic provisioning,
-- owner invitation, and terminal-retention foundation.

INSERT INTO permissions (name, display_name, scope, delegation_scope)
VALUES
    ('platform.registration_requests.read', 'Read tenant registration requests', 'platform', 'none'),
    ('platform.registration_requests.manage', 'Manage tenant registration requests', 'platform', 'none'),
    ('platform.tenants.provision', 'Provision tenants from reviewed registrations', 'platform', 'none')
ON CONFLICT (name) DO UPDATE
SET display_name = EXCLUDED.display_name,
    scope = EXCLUDED.scope,
    delegation_scope = EXCLUDED.delegation_scope,
    updated_at = now();

INSERT INTO role_permissions (role_id, permission_id)
SELECT role.id, permission.id
FROM roles AS role
JOIN permissions AS permission ON (
    role.name = 'platform_admin'
    AND permission.name IN (
        'platform.registration_requests.read',
        'platform.registration_requests.manage',
        'platform.tenants.provision'
    )
) OR (
    role.name = 'platform_ops'
    AND permission.name IN (
        'platform.registration_requests.read',
        'platform.registration_requests.manage'
    )
)
ON CONFLICT DO NOTHING;

CREATE OR REPLACE FUNCTION public.app_global_platform_capability(required_capability TEXT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM public.users account
        JOIN public.platform_role_assignments assignment
          ON assignment.user_id = account.id
         AND assignment.status = 'active'
        JOIN public.role_permissions role_permission
          ON role_permission.role_id = assignment.role_id
        JOIN public.permissions permission
          ON permission.id = role_permission.permission_id
         AND permission.name = required_capability
        WHERE account.id = public.app_request_actor_user_id()
          AND account.status = 'active'
          AND account.principal_scope = 'platform'
    )
$$;

REVOKE ALL ON FUNCTION public.app_global_platform_capability(TEXT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.app_global_platform_capability(TEXT) TO PUBLIC;

CREATE TABLE tenant_registration_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    public_reference TEXT NOT NULL UNIQUE,
    submission_key UUID NOT NULL UNIQUE,
    submission_payload_fingerprint TEXT NOT NULL CHECK (submission_payload_fingerprint ~ '^[0-9a-f]{64}$'),
    status TEXT NOT NULL DEFAULT 'new'
        CHECK (status IN ('new','in_review','qualified','setup_in_progress','converted','declined','spam')),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    assigned_to_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    converted_salon_id UUID REFERENCES salons(id) ON DELETE RESTRICT,
    converted_at TIMESTAMPTZ,
    terminal_at TIMESTAMPTZ,

    contact_full_name TEXT,
    contact_email TEXT,
    contact_email_normalized TEXT,
    contact_phone TEXT,
    contact_phone_normalized TEXT,
    salon_name TEXT,
    salon_phone TEXT,
    salon_phone_normalized TEXT,
    salon_website TEXT,
    city TEXT,
    state TEXT,
    zip_code TEXT,

    location_count INTEGER CHECK (location_count IS NULL OR location_count BETWEEN 1 AND 100),
    preferred_contact_language TEXT CHECK (preferred_contact_language IS NULL OR preferred_contact_language IN ('en','vi')),
    current_booking_system TEXT,
    estimated_weekly_call_volume TEXT,
    requested_help TEXT,
    notes TEXT,

    locale TEXT NOT NULL CHECK (locale IN ('en','vi')),
    source_page TEXT NOT NULL CHECK (source_page IN ('home','pricing')),
    marketing_plan_interest TEXT CHECK (marketing_plan_interest IS NULL OR marketing_plan_interest IN ('starter','growth','custom')),
    consent_version TEXT NOT NULL CHECK (consent_version = 'tenant-registration-contact-v1'),
    consent_at TIMESTAMPTZ NOT NULL,
    possible_duplicate BOOLEAN NOT NULL DEFAULT false,

    provisioning_draft JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(provisioning_draft) = 'object'),
    provisioning_draft_updated_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    provisioning_draft_updated_at TIMESTAMPTZ,

    retention_expires_at TIMESTAMPTZ,
    redacted_at TIMESTAMPTZ,
    redaction_version TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT tenant_registration_public_reference_shape
        CHECK (public_reference ~ '^MR-[A-Z0-9]{16}$'),
    CONSTRAINT tenant_registration_conversion_shape
        CHECK ((status = 'converted') = (converted_salon_id IS NOT NULL AND converted_at IS NOT NULL)),
    CONSTRAINT tenant_registration_terminal_retention_shape
        CHECK (
            (status IN ('converted','declined','spam') AND terminal_at IS NOT NULL AND retention_expires_at IS NOT NULL)
            OR
            (status NOT IN ('converted','declined','spam') AND terminal_at IS NULL AND retention_expires_at IS NULL)
        ),
    CONSTRAINT tenant_registration_redaction_shape
        CHECK ((redacted_at IS NULL AND redaction_version IS NULL) OR (redacted_at IS NOT NULL AND redaction_version IS NOT NULL)),
    CONSTRAINT tenant_registration_provisioning_draft_shape
        CHECK ((provisioning_draft_updated_at IS NULL) = (provisioning_draft_updated_by_user_id IS NULL)),
    CONSTRAINT tenant_registration_required_intake_or_redacted
        CHECK (
            redacted_at IS NOT NULL OR (
                NULLIF(contact_full_name, '') IS NOT NULL
                AND NULLIF(contact_email, '') IS NOT NULL
                AND NULLIF(contact_email_normalized, '') IS NOT NULL
                AND NULLIF(contact_phone, '') IS NOT NULL
                AND NULLIF(contact_phone_normalized, '') IS NOT NULL
                AND NULLIF(salon_name, '') IS NOT NULL
                AND NULLIF(salon_phone, '') IS NOT NULL
                AND NULLIF(salon_phone_normalized, '') IS NOT NULL
                AND NULLIF(city, '') IS NOT NULL
                AND NULLIF(state, '') IS NOT NULL
                AND NULLIF(zip_code, '') IS NOT NULL
                AND location_count IS NOT NULL
                AND preferred_contact_language IS NOT NULL
            )
        ),
    CONSTRAINT tenant_registration_intake_bounds CHECK (
        (contact_full_name IS NULL OR char_length(contact_full_name) BETWEEN 1 AND 160)
        AND (contact_email IS NULL OR char_length(contact_email) BETWEEN 3 AND 320)
        AND (contact_email_normalized IS NULL OR (char_length(contact_email_normalized) BETWEEN 3 AND 320 AND contact_email_normalized = lower(contact_email_normalized)))
        AND (contact_phone IS NULL OR char_length(contact_phone) BETWEEN 7 AND 32)
        AND (contact_phone_normalized IS NULL OR contact_phone_normalized ~ '^\+1[0-9]{10}$')
        AND (salon_name IS NULL OR char_length(salon_name) BETWEEN 1 AND 200)
        AND (salon_phone IS NULL OR char_length(salon_phone) BETWEEN 7 AND 32)
        AND (salon_phone_normalized IS NULL OR salon_phone_normalized ~ '^\+1[0-9]{10}$')
        AND (salon_website IS NULL OR char_length(salon_website) <= 2048)
        AND (city IS NULL OR char_length(city) BETWEEN 1 AND 120)
        AND (state IS NULL OR state ~ '^[A-Z]{2}$')
        AND (zip_code IS NULL OR zip_code ~ '^[0-9]{5}(-[0-9]{4})?$')
        AND (current_booking_system IS NULL OR char_length(current_booking_system) <= 160)
        AND (estimated_weekly_call_volume IS NULL OR char_length(estimated_weekly_call_volume) <= 160)
        AND (requested_help IS NULL OR char_length(requested_help) <= 4000)
        AND (notes IS NULL OR char_length(notes) <= 4000)
    )
);

CREATE UNIQUE INDEX idx_tenant_registration_converted_salon
    ON tenant_registration_requests (converted_salon_id)
    WHERE converted_salon_id IS NOT NULL;
CREATE INDEX idx_tenant_registration_status_created
    ON tenant_registration_requests (status, created_at DESC, id);
CREATE INDEX idx_tenant_registration_assignee_status
    ON tenant_registration_requests (assigned_to_user_id, status, created_at DESC);
CREATE INDEX idx_tenant_registration_email
    ON tenant_registration_requests (contact_email_normalized)
    WHERE contact_email_normalized IS NOT NULL;
CREATE INDEX idx_tenant_registration_contact_phone
    ON tenant_registration_requests (contact_phone_normalized)
    WHERE contact_phone_normalized IS NOT NULL;
CREATE INDEX idx_tenant_registration_salon_phone
    ON tenant_registration_requests (salon_phone_normalized)
    WHERE salon_phone_normalized IS NOT NULL;
CREATE INDEX idx_tenant_registration_retention_due
    ON tenant_registration_requests (retention_expires_at, id)
    WHERE retention_expires_at IS NOT NULL AND redacted_at IS NULL;

CREATE TABLE tenant_registration_request_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES tenant_registration_requests(id) ON DELETE RESTRICT,
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    from_status TEXT,
    to_status TEXT,
    request_version BIGINT NOT NULL CHECK (request_version >= 1),
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (request_id, request_version, event_type)
);
CREATE INDEX idx_tenant_registration_events_request
    ON tenant_registration_request_events (request_id, created_at, id);

CREATE TABLE tenant_registration_request_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES tenant_registration_requests(id) ON DELETE RESTRICT,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    action_key TEXT NOT NULL CHECK (length(action_key) BETWEEN 1 AND 256),
    action_type TEXT NOT NULL,
    request_fingerprint TEXT NOT NULL CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    result_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (request_id, action_key)
);

CREATE TABLE tenant_registration_request_notes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES tenant_registration_requests(id) ON DELETE RESTRICT,
    author_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    request_version BIGINT NOT NULL CHECK (request_version >= 1),
    content TEXT,
    redacted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((redacted_at IS NULL AND NULLIF(content, '') IS NOT NULL) OR (redacted_at IS NOT NULL AND content IS NULL))
);
CREATE INDEX idx_tenant_registration_notes_request
    ON tenant_registration_request_notes (request_id, created_at, id);

CREATE TABLE tenant_owner_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES tenant_registration_requests(id) ON DELETE RESTRICT,
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','used','revoked')),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (status = 'active' AND used_at IS NULL AND revoked_at IS NULL)
        OR (status = 'used' AND used_at IS NOT NULL AND revoked_at IS NULL)
        OR (status = 'revoked' AND revoked_at IS NOT NULL AND used_at IS NULL)
    )
);
CREATE UNIQUE INDEX idx_tenant_owner_invitations_one_active
    ON tenant_owner_invitations (user_id)
    WHERE status = 'active';
CREATE INDEX idx_tenant_owner_invitations_request
    ON tenant_owner_invitations (request_id, created_at DESC);

CREATE OR REPLACE FUNCTION public.tenant_registration_assignee_guard()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
BEGIN
    IF NEW.assigned_to_user_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM public.users account
        JOIN public.platform_role_assignments assignment
          ON assignment.user_id = account.id AND assignment.status = 'active'
        WHERE account.id = NEW.assigned_to_user_id
          AND account.status = 'active'
          AND account.principal_scope = 'platform'
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '23514', CONSTRAINT = 'tenant_registration_assignee_platform_guard';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER tenant_registration_assignee_platform_guard
BEFORE INSERT OR UPDATE OF assigned_to_user_id ON tenant_registration_requests
FOR EACH ROW EXECUTE FUNCTION public.tenant_registration_assignee_guard();

CREATE OR REPLACE FUNCTION public.reject_tenant_registration_immutable_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = TG_TABLE_NAME || ' is immutable';
END
$$;

CREATE TRIGGER tenant_registration_events_immutable
BEFORE UPDATE OR DELETE ON tenant_registration_request_events
FOR EACH ROW EXECUTE FUNCTION public.reject_tenant_registration_immutable_change();
CREATE TRIGGER tenant_registration_actions_immutable
BEFORE UPDATE OR DELETE ON tenant_registration_request_actions
FOR EACH ROW EXECUTE FUNCTION public.reject_tenant_registration_immutable_change();

CREATE OR REPLACE FUNCTION public.tenant_registration_notes_immutable_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'tenant registration notes are immutable';
    END IF;
    IF OLD.content IS NOT NULL
       AND NEW.content IS NULL
       AND OLD.redacted_at IS NULL
       AND NEW.redacted_at IS NOT NULL
       AND OLD.request_id = NEW.request_id
       AND OLD.author_user_id IS NOT DISTINCT FROM NEW.author_user_id
       AND OLD.request_version = NEW.request_version
       AND OLD.created_at = NEW.created_at THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'tenant registration notes are immutable';
END
$$;

CREATE TRIGGER tenant_registration_notes_immutable
BEFORE UPDATE OR DELETE ON tenant_registration_request_notes
FOR EACH ROW EXECUTE FUNCTION public.tenant_registration_notes_immutable_guard();

CREATE OR REPLACE FUNCTION public.create_tenant_registration_request(
    requested_id UUID,
    requested_reference TEXT,
    requested_submission_key UUID,
    requested_fingerprint TEXT,
    requested_contact_full_name TEXT,
    requested_contact_email TEXT,
    requested_contact_email_normalized TEXT,
    requested_contact_phone TEXT,
    requested_contact_phone_normalized TEXT,
    requested_salon_name TEXT,
    requested_salon_phone TEXT,
    requested_salon_phone_normalized TEXT,
    requested_salon_website TEXT,
    requested_city TEXT,
    requested_state TEXT,
    requested_zip_code TEXT,
    requested_location_count INTEGER,
    requested_preferred_contact_language TEXT,
    requested_current_booking_system TEXT,
    requested_estimated_weekly_call_volume TEXT,
    requested_help TEXT,
    requested_notes TEXT,
    requested_locale TEXT,
    requested_source_page TEXT,
    requested_plan_interest TEXT,
    requested_consent_version TEXT
)
RETURNS TABLE(public_reference TEXT, replayed BOOLEAN)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
DECLARE
    inserted_reference TEXT;
    existing_fingerprint TEXT;
    duplicate_evidence BOOLEAN;
BEGIN
    IF public.app_database_scope() <> 'public' THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'public registration scope required';
    END IF;

    duplicate_evidence := EXISTS (
        SELECT 1 FROM public.tenant_registration_requests existing
        WHERE existing.redacted_at IS NULL
          AND (
              existing.contact_email_normalized = requested_contact_email_normalized
              OR existing.contact_phone_normalized = requested_contact_phone_normalized
              OR existing.salon_phone_normalized = requested_salon_phone_normalized
          )
    );

    INSERT INTO public.tenant_registration_requests (
        id, public_reference, submission_key, submission_payload_fingerprint,
        contact_full_name, contact_email, contact_email_normalized,
        contact_phone, contact_phone_normalized, salon_name, salon_phone,
        salon_phone_normalized, salon_website, city, state, zip_code,
        location_count, preferred_contact_language, current_booking_system,
        estimated_weekly_call_volume, requested_help, notes, locale,
        source_page, marketing_plan_interest, consent_version, consent_at,
        possible_duplicate
    ) VALUES (
        requested_id, requested_reference, requested_submission_key, requested_fingerprint,
        requested_contact_full_name, requested_contact_email, requested_contact_email_normalized,
        requested_contact_phone, requested_contact_phone_normalized, requested_salon_name,
        requested_salon_phone, requested_salon_phone_normalized,
        NULLIF(requested_salon_website, ''), requested_city, requested_state,
        requested_zip_code, requested_location_count, requested_preferred_contact_language,
        NULLIF(requested_current_booking_system, ''),
        NULLIF(requested_estimated_weekly_call_volume, ''), NULLIF(requested_help, ''),
        NULLIF(requested_notes, ''), requested_locale, requested_source_page,
        NULLIF(requested_plan_interest, ''), requested_consent_version, now(), duplicate_evidence
    )
    ON CONFLICT (submission_key) DO NOTHING
    RETURNING tenant_registration_requests.public_reference INTO inserted_reference;

    IF inserted_reference IS NOT NULL THEN
        INSERT INTO public.tenant_registration_request_events (
            request_id, event_type, to_status, request_version, details
        ) VALUES (requested_id, 'submitted', 'new', 1, '{}'::jsonb);
        public_reference := inserted_reference;
        replayed := false;
        RETURN NEXT;
        RETURN;
    END IF;

    SELECT existing.public_reference, existing.submission_payload_fingerprint
      INTO public_reference, existing_fingerprint
    FROM public.tenant_registration_requests existing
    WHERE existing.submission_key = requested_submission_key;

    IF existing_fingerprint IS DISTINCT FROM requested_fingerprint THEN
        RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'TENANT_REGISTRATION_SUBMISSION_CONFLICT';
    END IF;
    replayed := true;
    RETURN NEXT;
END
$$;

REVOKE ALL ON FUNCTION public.create_tenant_registration_request(
    UUID,TEXT,UUID,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,
    INTEGER,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.create_tenant_registration_request(
    UUID,TEXT,UUID,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,
    INTEGER,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT
) TO PUBLIC;

CREATE OR REPLACE FUNCTION public.accept_tenant_owner_invitation(
    requested_token_hash TEXT,
    requested_password_hash TEXT
)
RETURNS TABLE(user_id UUID)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
DECLARE
    invitation_record public.tenant_owner_invitations%ROWTYPE;
BEGIN
    IF public.app_database_scope() <> 'public' THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'public invitation scope required';
    END IF;
    SELECT * INTO invitation_record
    FROM public.tenant_owner_invitations invitation
    WHERE invitation.token_hash = requested_token_hash
    FOR UPDATE;
    IF NOT FOUND OR invitation_record.status <> 'active' OR invitation_record.expires_at <= now() THEN
        RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'OWNER_INVITATION_INVALID';
    END IF;
    UPDATE public.users
    SET password_hash = requested_password_hash, status = 'active', updated_at = now()
    WHERE id = invitation_record.user_id
      AND principal_scope = 'tenant'
      AND status = 'invited';
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'OWNER_INVITATION_INVALID';
    END IF;
    UPDATE public.tenant_owner_invitations
    SET status = 'used', used_at = now(), updated_at = now()
    WHERE id = invitation_record.id;
    UPDATE public.refresh_tokens SET revoked_at = now()
    WHERE refresh_tokens.user_id = invitation_record.user_id AND revoked_at IS NULL;
    user_id := invitation_record.user_id;
    RETURN NEXT;
END
$$;

REVOKE ALL ON FUNCTION public.accept_tenant_owner_invitation(TEXT,TEXT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.accept_tenant_owner_invitation(TEXT,TEXT) TO PUBLIC;

CREATE OR REPLACE FUNCTION public.redact_due_tenant_registration_requests(requested_limit INTEGER)
RETURNS INTEGER
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp
SET row_security = off
AS $$
DECLARE
    affected_count INTEGER := 0;
BEGIN
    IF public.app_database_scope() <> 'worker' THEN
        RAISE EXCEPTION USING ERRCODE = '42501', MESSAGE = 'worker scope required';
    END IF;
    IF requested_limit < 1 OR requested_limit > 500 THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid retention batch limit';
    END IF;

    WITH due AS (
        SELECT request.id
        FROM public.tenant_registration_requests request
        WHERE request.status IN ('converted','declined','spam')
          AND request.retention_expires_at <= now()
          AND request.redacted_at IS NULL
        ORDER BY request.retention_expires_at, request.id
        FOR UPDATE SKIP LOCKED
        LIMIT requested_limit
    ), redacted AS (
        UPDATE public.tenant_registration_requests request
        SET contact_full_name = NULL,
            contact_email = NULL,
            contact_email_normalized = NULL,
            contact_phone = NULL,
            contact_phone_normalized = NULL,
            salon_name = NULL,
            salon_phone = NULL,
            salon_phone_normalized = NULL,
            salon_website = NULL,
            city = NULL,
            state = NULL,
            zip_code = NULL,
            location_count = NULL,
            preferred_contact_language = NULL,
            current_booking_system = NULL,
            estimated_weekly_call_volume = NULL,
            requested_help = NULL,
            notes = NULL,
            provisioning_draft = '{}'::jsonb,
            provisioning_draft_updated_by_user_id = NULL,
            provisioning_draft_updated_at = NULL,
            assigned_to_user_id = NULL,
            redacted_at = now(),
            redaction_version = 'tenant-registration-redaction-v1',
            updated_at = now()
        FROM due
        WHERE request.id = due.id
        RETURNING request.id, request.redacted_at
    ), notes_redacted AS (
        UPDATE public.tenant_registration_request_notes note
        SET content = NULL, redacted_at = redacted.redacted_at
        FROM redacted
        WHERE note.request_id = redacted.id AND note.redacted_at IS NULL
        RETURNING note.id
    )
    SELECT count(*) INTO affected_count FROM redacted;
    RETURN affected_count;
END
$$;

REVOKE ALL ON FUNCTION public.redact_due_tenant_registration_requests(INTEGER) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.redact_due_tenant_registration_requests(INTEGER) TO PUBLIC;

ALTER TABLE tenant_registration_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_registration_request_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_registration_request_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_registration_request_notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_owner_invitations ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_registration_requests_read ON tenant_registration_requests
FOR SELECT USING (public.app_global_platform_capability('platform.registration_requests.read'));
CREATE POLICY tenant_registration_requests_manage ON tenant_registration_requests
FOR UPDATE USING (public.app_global_platform_capability('platform.registration_requests.manage'))
WITH CHECK (public.app_global_platform_capability('platform.registration_requests.manage'));

CREATE POLICY tenant_registration_events_read ON tenant_registration_request_events
FOR SELECT USING (public.app_global_platform_capability('platform.registration_requests.read'));
CREATE POLICY tenant_registration_events_insert ON tenant_registration_request_events
FOR INSERT WITH CHECK (public.app_global_platform_capability('platform.registration_requests.manage'));

CREATE POLICY tenant_registration_actions_read ON tenant_registration_request_actions
FOR SELECT USING (public.app_global_platform_capability('platform.registration_requests.manage'));
CREATE POLICY tenant_registration_actions_insert ON tenant_registration_request_actions
FOR INSERT WITH CHECK (public.app_global_platform_capability('platform.registration_requests.manage'));

CREATE POLICY tenant_registration_notes_read ON tenant_registration_request_notes
FOR SELECT USING (public.app_global_platform_capability('platform.registration_requests.read'));
CREATE POLICY tenant_registration_notes_insert ON tenant_registration_request_notes
FOR INSERT WITH CHECK (public.app_global_platform_capability('platform.registration_requests.manage'));

CREATE POLICY tenant_owner_invitations_read ON tenant_owner_invitations
FOR SELECT USING (public.app_global_platform_capability('platform.tenants.provision'));
CREATE POLICY tenant_owner_invitations_insert ON tenant_owner_invitations
FOR INSERT WITH CHECK (public.app_global_platform_capability('platform.tenants.provision'));
CREATE POLICY tenant_owner_invitations_update ON tenant_owner_invitations
FOR UPDATE USING (public.app_global_platform_capability('platform.tenants.provision'))
WITH CHECK (public.app_global_platform_capability('platform.tenants.provision'));

-- Platform provisioning creates a tenant-owned salon while preserving the
-- actual Platform actor in request context and audit events.
DROP POLICY IF EXISTS saas_rls_salons_insert ON public.salons;
CREATE POLICY saas_rls_salons_insert ON public.salons
FOR INSERT WITH CHECK (
    owner_user_id = public.app_request_actor_user_id()
    OR public.app_global_platform_capability('platform.tenants.provision')
);

COMMENT ON TABLE tenant_registration_requests IS
'Pre-tenant Platform registration aggregate. Intake text never configures providers, selects scheduling authority, or enables AI booking.';
COMMENT ON TABLE tenant_owner_invitations IS
'One-time owner invitation records. Only SHA-256 token hashes are persisted; raw tokens are returned once and never recoverable.';
