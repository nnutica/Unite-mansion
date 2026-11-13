-- +goose Up
CREATE TABLE announcements (
    id         uuid        PRIMARY KEY,
    title      text        NOT NULL,
    message    text        NOT NULL,
    source     text        NOT NULL CHECK (source IN ('UTILITY_RATE_CHANGED')),
    source_id  text        NOT NULL,
    created_at timestamptz NOT NULL,
    -- One announcement per rate change, even if the event is handled twice.
    CONSTRAINT announcements_source_uq UNIQUE (source, source_id)
);
CREATE INDEX announcements_created_idx ON announcements (created_at DESC);

-- Owner settings (P-A13): a single row.
CREATE TABLE settings (
    id                      boolean     PRIMARY KEY DEFAULT true CHECK (id),
    lease_expiry_alert_days int         NOT NULL DEFAULT 30 CHECK (lease_expiry_alert_days BETWEEN 1 AND 365),
    payment_qr_key          text,
    updated_at              timestamptz NOT NULL DEFAULT now()
);
INSERT INTO settings (id) VALUES (true);

-- +goose Down
DROP TABLE settings;
DROP TABLE announcements;
