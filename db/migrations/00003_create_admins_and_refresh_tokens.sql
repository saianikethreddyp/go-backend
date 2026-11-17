-- +goose Up
CREATE TABLE admins (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email           text        NOT NULL CHECK (email = lower(email) AND char_length(email) <= 254),
    name            text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    password_hash   text        NOT NULL,
    role            text        NOT NULL CHECK (role IN ('super_admin', 'hub_manager')),
    hub_id          uuid        REFERENCES hubs (id),
    active          boolean     NOT NULL DEFAULT true,
    failed_attempts integer     NOT NULL DEFAULT 0,
    locked_until    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT admins_email_key UNIQUE (email),
    CONSTRAINT admins_role_hub_check CHECK (
        (role = 'hub_manager' AND hub_id IS NOT NULL) OR
        (role = 'super_admin' AND hub_id IS NULL)
    )
);

CREATE TABLE refresh_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id   uuid        NOT NULL REFERENCES admins (id) ON DELETE CASCADE,
    family_id  uuid        NOT NULL,
    token_hash bytea       NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT refresh_tokens_hash_key UNIQUE (token_hash)
);

CREATE INDEX refresh_tokens_family_idx ON refresh_tokens (family_id);
CREATE INDEX refresh_tokens_admin_idx ON refresh_tokens (admin_id);

-- +goose Down
DROP TABLE refresh_tokens;
DROP TABLE admins;
