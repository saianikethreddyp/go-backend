-- +goose Up
-- Contacts are collected only to trace a car in case of theft or misuse.
-- payload is AES-256-GCM encrypted; phone_hash is an HMAC used to deduplicate.
CREATE TABLE user_contacts (
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    phone_hash bytea       NOT NULL,
    payload    bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    synced_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, phone_hash)
);

-- Record of the user's own deletion requests (no contact data is kept here).
CREATE TABLE contact_deletion_requests (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    deleted_count integer     NOT NULL,
    reason        text        CHECK (reason IS NULL OR char_length(reason) <= 500),
    requested_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX contact_deletion_requests_user_idx ON contact_deletion_requests (user_id);

-- Who looked at what. Written whenever an admin views contacts.
CREATE TABLE audit_log (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id    uuid        NOT NULL REFERENCES admins (id),
    action      text        NOT NULL,
    target_type text        NOT NULL,
    target_id   uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_target_idx ON audit_log (target_type, target_id);
CREATE INDEX audit_log_admin_idx ON audit_log (admin_id, created_at DESC);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE contact_deletion_requests;
DROP TABLE user_contacts;
