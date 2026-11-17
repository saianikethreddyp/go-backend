-- +goose Up
CREATE TABLE hubs (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text             NOT NULL CHECK (char_length(name) BETWEEN 2 AND 100),
    address    text             NOT NULL CHECK (char_length(address) BETWEEN 5 AND 300),
    latitude   double precision NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude  double precision NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    active     boolean          NOT NULL DEFAULT true,
    created_at timestamptz      NOT NULL DEFAULT now(),
    updated_at timestamptz      NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX hubs_name_lower_key ON hubs (lower(name));

-- +goose Down
DROP TABLE hubs;
