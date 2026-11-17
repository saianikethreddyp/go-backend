-- +goose Up
-- Single admin level: every admin is a full admin, so roles and hub scoping go.
ALTER TABLE admins DROP CONSTRAINT admins_role_hub_check;
ALTER TABLE admins DROP COLUMN role;
ALTER TABLE admins DROP COLUMN hub_id;

-- +goose Down
ALTER TABLE admins ADD COLUMN role text NOT NULL DEFAULT 'super_admin' CHECK (role IN ('super_admin', 'hub_manager'));
ALTER TABLE admins ADD COLUMN hub_id uuid REFERENCES hubs (id);
ALTER TABLE admins ADD CONSTRAINT admins_role_hub_check CHECK (
    (role = 'hub_manager' AND hub_id IS NOT NULL) OR
    (role = 'super_admin' AND hub_id IS NULL)
);
