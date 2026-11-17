-- +goose Up
ALTER TABLE admins ADD COLUMN last_login_at timestamptz;
ALTER TABLE admins ADD COLUMN must_change_password boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE admins DROP COLUMN must_change_password;
ALTER TABLE admins DROP COLUMN last_login_at;
