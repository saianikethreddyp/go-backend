-- +goose Up
-- Photos are now managed only through the upload endpoints and stored as
-- object-storage keys. Any previously stored external URLs are cleared.
UPDATE cars SET photos = '{}' WHERE photos <> '{}';

-- +goose Down
SELECT 1;
