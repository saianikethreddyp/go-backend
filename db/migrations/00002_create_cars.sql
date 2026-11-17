-- +goose Up
CREATE TABLE cars (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    hub_id        uuid        NOT NULL REFERENCES hubs (id),
    brand         text        NOT NULL CHECK (char_length(brand) BETWEEN 1 AND 60),
    model         text        NOT NULL CHECK (char_length(model) BETWEEN 1 AND 60),
    plate         text        NOT NULL,
    year          integer     NOT NULL CHECK (year BETWEEN 2000 AND 2100),
    fuel_type     text        NOT NULL CHECK (fuel_type IN ('petrol', 'diesel', 'electric', 'cng')),
    transmission  text        NOT NULL CHECK (transmission IN ('manual', 'automatic')),
    seats         integer     NOT NULL CHECK (seats BETWEEN 1 AND 20),
    price_per_day integer     NOT NULL CHECK (price_per_day > 0), -- INR, whole rupees
    photos        text[]      NOT NULL DEFAULT '{}' CHECK (cardinality(photos) <= 10),
    status        text        NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'maintenance', 'retired')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT cars_plate_key UNIQUE (plate)
);

CREATE INDEX cars_hub_status_idx ON cars (hub_id, status);

-- +goose Down
DROP TABLE cars;
