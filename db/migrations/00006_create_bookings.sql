-- +goose Up
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE bookings (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid        NOT NULL REFERENCES users (id),
    car_id        uuid        NOT NULL REFERENCES cars (id),
    hub_id        uuid        NOT NULL REFERENCES hubs (id),
    start_at      timestamptz NOT NULL,
    end_at        timestamptz NOT NULL,
    status        text        NOT NULL DEFAULT 'confirmed'
                  CHECK (status IN ('confirmed', 'active', 'completed', 'cancelled')),
    price_per_day integer     NOT NULL CHECK (price_per_day > 0), -- snapshot, INR whole rupees
    total_price   integer     NOT NULL CHECK (total_price > 0),
    started_at    timestamptz,
    completed_at  timestamptz,
    cancelled_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT bookings_time_check CHECK (end_at > start_at),
    -- A car can never have two overlapping open bookings, even under concurrency.
    CONSTRAINT bookings_no_overlap EXCLUDE USING gist (
        car_id WITH =,
        tstzrange(start_at, end_at) WITH &&
    ) WHERE (status IN ('confirmed', 'active'))
);

CREATE INDEX bookings_user_created_idx ON bookings (user_id, created_at DESC);
CREATE INDEX bookings_hub_status_idx ON bookings (hub_id, status);
CREATE INDEX bookings_car_idx ON bookings (car_id);

-- +goose Down
DROP TABLE bookings;
