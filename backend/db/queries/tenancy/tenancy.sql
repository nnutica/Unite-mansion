-- name: ListRooms :many
SELECT * FROM rooms ORDER BY floor, room_number::int;

-- name: GetRoomByNumber :one
SELECT * FROM rooms WHERE room_number = $1;

-- name: GetRoomByNumberForUpdate :one
SELECT * FROM rooms WHERE room_number = $1 FOR UPDATE;

-- name: GetRoomByID :one
SELECT * FROM rooms WHERE id = $1;

-- name: UpdateRoom :exec
UPDATE rooms
SET base_price = $2, electric_meter_digits = $3, water_meter_digits = $4, updated_at = now()
WHERE id = $1;

-- name: ListLeasesByRoom :many
SELECT * FROM leases WHERE room_id = $1 ORDER BY start_date DESC;

-- name: ListLeasesActiveOn :many
SELECT * FROM leases WHERE start_date <= sqlc.arg(day)::date AND end_date >= sqlc.arg(day)::date;

-- name: CountLeasesCoveringRange :one
SELECT count(*) FROM leases
WHERE room_id = $1 AND start_date <= sqlc.arg(last_day)::date AND end_date >= sqlc.arg(first_day)::date;

-- name: GetLeaseForUpdate :one
SELECT * FROM leases WHERE id = $1 FOR UPDATE;

-- name: InsertLease :exec
INSERT INTO leases (id, room_id, tenant_name, start_date, end_date) VALUES ($1, $2, $3, $4, $5);

-- name: UpdateLease :exec
UPDATE leases SET tenant_name = $2, start_date = $3, end_date = $4, updated_at = now() WHERE id = $1;

-- name: ListRecurringChargesByRoom :many
SELECT * FROM recurring_charges WHERE room_id = $1 ORDER BY created_at;

-- name: GetRecurringChargeForUpdate :one
SELECT * FROM recurring_charges WHERE id = $1 FOR UPDATE;

-- name: InsertRecurringCharge :exec
INSERT INTO recurring_charges (id, room_id, name, amount, active) VALUES ($1, $2, $3, $4, $5);

-- name: UpdateRecurringCharge :exec
UPDATE recurring_charges SET name = $2, amount = $3, active = $4, updated_at = now() WHERE id = $1;
