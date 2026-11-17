-- +goose Up
CREATE TABLE users (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    phone              text        NOT NULL CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'),
    name               text        CHECK (name IS NULL OR char_length(name) BETWEEN 1 AND 100),
    active             boolean     NOT NULL DEFAULT true,
    contacts_synced_at timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_phone_key UNIQUE (phone)
);

CREATE TABLE user_refresh_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    family_id  uuid        NOT NULL,
    token_hash bytea       NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_refresh_tokens_hash_key UNIQUE (token_hash)
);

CREATE INDEX user_refresh_tokens_family_idx ON user_refresh_tokens (family_id);
CREATE INDEX user_refresh_tokens_user_idx ON user_refresh_tokens (user_id);

-- One-time codes. Only an HMAC of the code is stored.
CREATE TABLE otp_codes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    phone       text        NOT NULL,
    intent      text        NOT NULL CHECK (intent IN ('login', 'signup')),
    code_hash   bytea       NOT NULL,
    expires_at  timestamptz NOT NULL,
    attempts    integer     NOT NULL DEFAULT 0,
    consumed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX otp_codes_phone_created_idx ON otp_codes (phone, created_at DESC);

-- +goose Down
DROP TABLE otp_codes;
DROP TABLE user_refresh_tokens;
DROP TABLE users;
