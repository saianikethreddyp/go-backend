-- +goose Up
CREATE TABLE devices (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    fcm_token    text        NOT NULL CHECK (char_length(fcm_token) BETWEEN 20 AND 4096),
    platform     text        NOT NULL CHECK (platform IN ('android', 'ios')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT devices_token_key UNIQUE (fcm_token)
);

CREATE INDEX devices_user_idx ON devices (user_id);

-- +goose Down
DROP TABLE devices;
