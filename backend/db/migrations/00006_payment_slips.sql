-- +goose Up
CREATE TABLE payment_slips (
    id            uuid        PRIMARY KEY,
    invoice_id    uuid        NOT NULL REFERENCES invoices (id),
    file_key      text        NOT NULL UNIQUE,
    file_sha256   text        NOT NULL,
    content_type  text        NOT NULL,
    size_bytes    bigint      NOT NULL CHECK (size_bytes > 0),
    status        text        NOT NULL CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED')),
    reject_reason text        CHECK (reject_reason IN ('AMOUNT_MISMATCH', 'DUPLICATE', 'UNCLEAR')),
    reject_note   text,
    submitted_at  timestamptz NOT NULL,
    reviewed_at   timestamptz,
    -- §6.4: a rejection always has a reason.
    CONSTRAINT payment_slips_reject_reason_ck CHECK ((status = 'REJECTED') = (reject_reason IS NOT NULL))
);
CREATE INDEX payment_slips_invoice_idx ON payment_slips (invoice_id);
CREATE INDEX payment_slips_status_idx ON payment_slips (status, submitted_at);
CREATE INDEX payment_slips_sha256_idx ON payment_slips (file_sha256);

-- +goose Down
DROP TABLE payment_slips;
