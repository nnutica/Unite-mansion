-- +goose Up
CREATE TABLE audit_logs (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    event_name   text        NOT NULL,
    aggregate_id text        NOT NULL,
    actor_type   text        NOT NULL CHECK (actor_type IN ('ADMIN', 'TENANT', 'SYSTEM')),
    actor_id     text,
    payload      jsonb       NOT NULL,
    occurred_at  timestamptz NOT NULL,
    recorded_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_aggregate_idx ON audit_logs (aggregate_id, occurred_at);
CREATE INDEX audit_logs_occurred_at_idx ON audit_logs (occurred_at DESC);

-- +goose Down
DROP TABLE audit_logs;
