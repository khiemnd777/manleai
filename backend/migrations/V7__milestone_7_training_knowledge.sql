CREATE TABLE knowledge_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'faq' CHECK (category IN ('faq', 'policy', 'services', 'hours', 'handoff', 'operations')),
    body TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'archived')),
    source TEXT NOT NULL DEFAULT 'owner' CHECK (source IN ('owner', 'correction')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_knowledge_items_salon_status_updated
    ON knowledge_items(salon_id, status, updated_at DESC);

CREATE INDEX idx_knowledge_items_salon_category
    ON knowledge_items(salon_id, category, updated_at DESC);

CREATE TABLE owner_corrections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
    call_session_id UUID REFERENCES call_sessions(id) ON DELETE SET NULL,
    transcript_message_id UUID REFERENCES call_transcript_messages(id) ON DELETE SET NULL,
    correction TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'dismissed')),
    applied_knowledge_item_id UUID REFERENCES knowledge_items(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_owner_corrections_salon_status_created
    ON owner_corrections(salon_id, status, created_at DESC);

CREATE INDEX idx_owner_corrections_call_session
    ON owner_corrections(call_session_id, created_at DESC);
