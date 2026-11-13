-- +goose Up
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- tenancy ----------------------------------------------------------------

CREATE TABLE rooms (
    id                    uuid          PRIMARY KEY DEFAULT gen_random_uuid(),
    room_number           text          NOT NULL UNIQUE,
    floor                 int           NOT NULL CHECK (floor BETWEEN 1 AND 99),
    base_price            numeric(12,2) NOT NULL CHECK (base_price > 0),
    electric_meter_digits int           CHECK (electric_meter_digits BETWEEN 1 AND 9),
    water_meter_digits    int           CHECK (water_meter_digits BETWEEN 1 AND 9),
    created_at            timestamptz   NOT NULL DEFAULT now(),
    updated_at            timestamptz   NOT NULL DEFAULT now()
);

CREATE TABLE leases (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id     uuid        NOT NULL REFERENCES rooms (id),
    tenant_name text        NOT NULL CHECK (length(tenant_name) BETWEEN 1 AND 200),
    start_date  date        NOT NULL,
    end_date    date        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT leases_dates_ck CHECK (end_date >= start_date),
    -- §6.6: a room cannot have leases with overlapping periods.
    CONSTRAINT leases_no_overlap EXCLUDE USING gist (
        room_id WITH =,
        daterange(start_date, end_date, '[]') WITH &&
    )
);

CREATE TABLE recurring_charges (
    id         uuid          PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id    uuid          NOT NULL REFERENCES rooms (id),
    name       text          NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    amount     numeric(12,2) NOT NULL CHECK (amount > 0),
    active     boolean       NOT NULL DEFAULT true,
    created_at timestamptz   NOT NULL DEFAULT now(),
    updated_at timestamptz   NOT NULL DEFAULT now()
);
CREATE INDEX recurring_charges_room_idx ON recurring_charges (room_id);

-- identity ---------------------------------------------------------------

CREATE TABLE admins (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    username      text        NOT NULL UNIQUE,
    password_hash text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
-- §6.5: exactly one admin account.
CREATE UNIQUE INDEX admins_singleton ON admins ((true));

CREATE TABLE tenant_accounts (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    username      text        NOT NULL UNIQUE,
    password_hash text        NOT NULL,
    full_name     text        NOT NULL,
    phone         text        NOT NULL,
    status        text        NOT NULL CHECK (status IN ('PENDING', 'ACTIVE', 'INACTIVE')),
    room_id       uuid        REFERENCES rooms (id),
    unassigned_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    -- §6.5: ACTIVE ⇔ bound to exactly one room.
    CONSTRAINT tenant_accounts_active_room_ck CHECK ((status = 'ACTIVE') = (room_id IS NOT NULL))
);
-- §6.5: one account per room.
CREATE UNIQUE INDEX tenant_accounts_one_active_per_room ON tenant_accounts (room_id) WHERE status = 'ACTIVE';
CREATE INDEX tenant_accounts_status_idx ON tenant_accounts (status);

CREATE TABLE sessions (
    token_hash   bytea       PRIMARY KEY,
    subject_type text        NOT NULL CHECK (subject_type IN ('ADMIN', 'TENANT')),
    subject_id   uuid        NOT NULL,
    expires_at   timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_subject_idx ON sessions (subject_type, subject_id);

-- +goose Down
DROP TABLE sessions;
DROP TABLE tenant_accounts;
DROP TABLE admins;
DROP TABLE recurring_charges;
DROP TABLE leases;
DROP TABLE rooms;
