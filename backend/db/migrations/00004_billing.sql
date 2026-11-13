-- +goose Up
CREATE TABLE utility_rates (
    id             uuid          PRIMARY KEY DEFAULT gen_random_uuid(),
    utility_type   text          NOT NULL CHECK (utility_type IN ('ELECTRICITY', 'WATER')),
    per_unit       numeric(12,2) NOT NULL CHECK (per_unit > 0),
    effective_from date          NOT NULL,
    created_at     timestamptz   NOT NULL DEFAULT now(),
    -- §6.3: one effective rate per type on any given day.
    CONSTRAINT utility_rates_type_from_uq UNIQUE (utility_type, effective_from)
);

CREATE TABLE invoices (
    id                    uuid          PRIMARY KEY,
    invoice_number        text          NOT NULL UNIQUE,
    room_id               uuid          NOT NULL REFERENCES rooms (id),
    room_number           text          NOT NULL,
    billing_period        text          NOT NULL CHECK (billing_period ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
    status                text          NOT NULL CHECK (status IN ('DRAFT', 'ISSUED', 'PAID')),
    -- INV-4 snapshot taken when the draft is created.
    rent_amount           numeric(12,2) NOT NULL,
    electric_rate         numeric(12,2) NOT NULL,
    water_rate            numeric(12,2) NOT NULL,
    electric_meter_digits int           NOT NULL,
    water_meter_digits    int           NOT NULL,
    -- Meter readings; previous may be NULL until entered (first bill, INV-8).
    electric_previous     bigint,
    electric_current      bigint,
    electric_rollover     boolean       NOT NULL DEFAULT false,
    water_previous        bigint,
    water_current         bigint,
    water_rollover        boolean       NOT NULL DEFAULT false,
    total                 numeric(12,2) NOT NULL,
    issued_at             timestamptz,
    paid_at               timestamptz,
    created_at            timestamptz   NOT NULL DEFAULT now(),
    updated_at            timestamptz   NOT NULL DEFAULT now(),
    -- INV-1: one invoice per room per billing period.
    CONSTRAINT invoices_room_period_uq UNIQUE (room_id, billing_period)
);
CREATE INDEX invoices_period_idx ON invoices (billing_period);

CREATE TABLE invoice_lines (
    id          uuid          PRIMARY KEY,
    invoice_id  uuid          NOT NULL REFERENCES invoices (id) ON DELETE CASCADE,
    position    int           NOT NULL,
    line_type   text          NOT NULL CHECK (line_type IN ('RENT', 'ELECTRICITY', 'WATER', 'RECURRING', 'DISCOUNT', 'PENALTY')),
    description text          NOT NULL,
    amount      numeric(12,2) NOT NULL,
    CONSTRAINT invoice_lines_position_uq UNIQUE (invoice_id, position)
);

-- +goose Down
DROP TABLE invoice_lines;
DROP TABLE invoices;
DROP TABLE utility_rates;
