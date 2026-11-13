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

-- name: ListInvoiceSummariesByRoom :many
SELECT i.id, i.invoice_number, i.room_number, i.billing_period, i.total, i.status,
       EXISTS (SELECT 1 FROM payment_slips s WHERE s.invoice_id = i.id AND s.status = 'PENDING')::bool AS has_pending_slip
FROM invoices i
WHERE i.room_id = $1 AND i.status <> 'DRAFT'
ORDER BY i.billing_period DESC;

-- name: InsertSlip :exec
INSERT INTO payment_slips (id, invoice_id, file_key, file_sha256, content_type, size_bytes, status, submitted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: UpdateSlipReview :exec
UPDATE payment_slips SET status = $2, reject_reason = $3, reject_note = $4, reviewed_at = $5 WHERE id = $1;

-- name: GetSlipForUpdate :one
SELECT * FROM payment_slips WHERE id = $1 FOR UPDATE;

-- name: ListSlipsByInvoice :many
SELECT sqlc.embed(s),
       EXISTS (SELECT 1 FROM payment_slips o WHERE o.file_sha256 = s.file_sha256 AND o.id <> s.id)::bool AS possible_duplicate
FROM payment_slips s
WHERE s.invoice_id = $1
ORDER BY s.submitted_at;

-- name: ListSlipQueue :many
SELECT sqlc.embed(s),
       EXISTS (SELECT 1 FROM payment_slips o WHERE o.file_sha256 = s.file_sha256 AND o.id <> s.id)::bool AS possible_duplicate,
       i.invoice_number, i.room_number, i.billing_period, i.total, i.status AS invoice_status,
       EXISTS (SELECT 1 FROM payment_slips p WHERE p.invoice_id = i.id AND p.status = 'PENDING')::bool AS has_pending_slip
FROM payment_slips s
JOIN invoices i ON i.id = s.invoice_id
WHERE sqlc.narg(status)::text IS NULL OR s.status = sqlc.narg(status)::text
ORDER BY s.submitted_at;

-- name: GetSlipQueueItem :one
SELECT sqlc.embed(s),
       EXISTS (SELECT 1 FROM payment_slips o WHERE o.file_sha256 = s.file_sha256 AND o.id <> s.id)::bool AS possible_duplicate,
       i.invoice_number, i.room_number, i.billing_period, i.total, i.status AS invoice_status,
       EXISTS (SELECT 1 FROM payment_slips p WHERE p.invoice_id = i.id AND p.status = 'PENDING')::bool AS has_pending_slip
FROM payment_slips s
JOIN invoices i ON i.id = s.invoice_id
WHERE s.id = $1;

-- name: ListPeriodRoomStatus :many
-- Read model for P-A03 (context.md §4 allows cross-table reads for dashboards/reports).
SELECT r.room_number, r.floor,
       EXISTS (SELECT 1 FROM leases l
               WHERE l.room_id = r.id
                 AND l.start_date <= sqlc.arg(last_day)::date
                 AND l.end_date >= sqlc.arg(first_day)::date)::bool AS has_lease,
       i.id AS invoice_id, i.invoice_number, i.total, i.status,
       EXISTS (SELECT 1 FROM payment_slips s WHERE s.invoice_id = i.id AND s.status = 'PENDING')::bool AS has_pending_slip
FROM rooms r
LEFT JOIN invoices i ON i.room_id = r.id AND i.billing_period = sqlc.arg(period)::text
ORDER BY r.floor, r.room_number::int;

-- name: CountPendingSlips :one
SELECT count(*) FROM payment_slips WHERE status = 'PENDING';
