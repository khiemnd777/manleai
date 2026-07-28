-- Data classification is schema-only. Sample fixtures are deliberately owned
-- by backend/sampledata and never run by the normal startup migration chain.
ALTER TABLE users
    ADD COLUMN data_classification TEXT NOT NULL DEFAULT 'live',
    ADD CONSTRAINT users_data_classification_check
        CHECK (data_classification IN ('live', 'sample_test')),
    ADD CONSTRAINT users_id_data_classification_key
        UNIQUE (id, data_classification);

ALTER TABLE salons
    ADD COLUMN data_classification TEXT NOT NULL DEFAULT 'live',
    ADD CONSTRAINT salons_data_classification_check
        CHECK (data_classification IN ('live', 'sample_test')),
    ADD CONSTRAINT salons_owner_data_classification_fk
        FOREIGN KEY (owner_user_id, data_classification)
        REFERENCES users(id, data_classification)
        ON DELETE RESTRICT;

CREATE INDEX idx_users_data_classification
    ON users (data_classification, status, created_at);

CREATE INDEX idx_salons_data_classification
    ON salons (data_classification, created_at);

CREATE FUNCTION enforce_data_classification_immutable()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.data_classification IS DISTINCT FROM NEW.data_classification THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'data classification is immutable',
            CONSTRAINT = TG_TABLE_NAME || '_data_classification_immutable_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_data_classification_immutable_guard
BEFORE UPDATE OF data_classification ON users
FOR EACH ROW
EXECUTE FUNCTION enforce_data_classification_immutable();

CREATE TRIGGER salons_data_classification_immutable_guard
BEFORE UPDATE OF data_classification ON salons
FOR EACH ROW
EXECUTE FUNCTION enforce_data_classification_immutable();

COMMENT ON COLUMN users.data_classification IS
'Marks live identities versus explicitly provisioned sample-test identities. Normal authentication bootstrap creates live identities by default.';

COMMENT ON COLUMN salons.data_classification IS
'Marks live tenants versus opt-in sample-test tenants. Normal schema migrations never insert sample tenants.';
