-- +goose Up
CREATE TABLE maintenance_requests (
    id          uuid        PRIMARY KEY,
    room_id     uuid        NOT NULL REFERENCES rooms (id),
    category    text        NOT NULL CHECK (category IN ('WATER', 'ELECTRICITY', 'AIR_CONDITIONER', 'FURNITURE')),
    description text        NOT NULL,
    status      text        NOT NULL CHECK (status IN ('NEW', 'COORDINATING', 'TECHNICIAN_DISPATCHED', 'COMPLETED')),
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);
CREATE INDEX maintenance_requests_room_idx ON maintenance_requests (room_id, created_at DESC);
CREATE INDEX maintenance_requests_status_idx ON maintenance_requests (status, created_at DESC);

CREATE TABLE maintenance_images (
    request_id uuid NOT NULL REFERENCES maintenance_requests (id) ON DELETE CASCADE,
    position   int  NOT NULL CHECK (position BETWEEN 0 AND 2), -- §6.6: at most 3 photos
    file_key   text NOT NULL UNIQUE,
    PRIMARY KEY (request_id, position)
);

-- +goose Down
DROP TABLE maintenance_images;
DROP TABLE maintenance_requests;
