-- name: ListUtilityRates :many
SELECT * FROM utility_rates ORDER BY effective_from DESC, utility_type;

-- name: GetLatestUtilityRate :one
SELECT * FROM utility_rates WHERE utility_type = $1 ORDER BY effective_from DESC LIMIT 1;

-- name: InsertUtilityRate :exec
INSERT INTO utility_rates (id, utility_type, per_unit, effective_from, created_at) VALUES ($1, $2, $3, $4, $5);

-- name: InsertInvoice :exec
INSERT INTO invoices (
    id, invoice_number, room_id, room_number, billing_period, status,
    rent_amount, electric_rate, water_rate, electric_meter_digits, water_meter_digits,
    electric_previous, electric_current, electric_rollover,
    water_previous, water_current, water_rollover,
    total, issued_at, paid_at, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14,
    $15, $16, $17,
    $18, $19, $20, $21, $21
);

-- name: UpdateInvoice :exec
UPDATE invoices SET
    status = $2,
    electric_previous = $3, electric_current = $4, electric_rollover = $5,
    water_previous = $6, water_current = $7, water_rollover = $8,
    total = $9, issued_at = $10, paid_at = $11, updated_at = now()
WHERE id = $1;

-- name: GetInvoice :one
SELECT * FROM invoices WHERE id = $1;

-- name: GetInvoiceForUpdate :one
SELECT * FROM invoices WHERE id = $1 FOR UPDATE;

-- name: GetInvoiceByRoomPeriod :one
SELECT * FROM invoices WHERE room_id = $1 AND billing_period = $2;

-- name: ListInvoiceLines :many
SELECT * FROM invoice_lines WHERE invoice_id = $1 ORDER BY position;

-- name: DeleteInvoiceLines :exec
DELETE FROM invoice_lines WHERE invoice_id = $1;

-- name: InsertInvoiceLine :exec
INSERT INTO invoice_lines (id, invoice_id, position, line_type, description, amount) VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListIssuedReadingsBefore :many
-- Latest issued (or paid) invoices of a room before a period, newest first:
-- the first row gives the default previous readings (INV-8), up to three rows
-- give the usage history for the abnormal-usage check (§6.2).
SELECT electric_previous, electric_current, electric_meter_digits,
       water_previous, water_current, water_meter_digits
FROM invoices
WHERE room_id = $1 AND billing_period < $2 AND status IN ('ISSUED', 'PAID')
ORDER BY billing_period DESC
LIMIT 3;
