-- name: InsertAnnouncement :exec
-- Idempotent per source event.
INSERT INTO announcements (id, title, message, source, source_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (source, source_id) DO NOTHING;

-- name: ListAnnouncements :many
SELECT * FROM announcements ORDER BY created_at DESC;

-- name: GetSettings :one
SELECT lease_expiry_alert_days, payment_qr_key FROM settings WHERE id;

-- name: UpdateSettings :exec
UPDATE settings SET lease_expiry_alert_days = $1, payment_qr_key = $2, updated_at = now() WHERE id;
