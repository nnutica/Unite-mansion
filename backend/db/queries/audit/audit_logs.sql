-- name: InsertAuditLog :exec
INSERT INTO audit_logs (event_name, aggregate_id, actor_type, actor_id, payload, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListAuditLogsByAggregate :many
SELECT id, event_name, aggregate_id, actor_type, actor_id, payload, occurred_at, recorded_at
FROM audit_logs
WHERE aggregate_id = $1
ORDER BY occurred_at, recorded_at;

-- name: ListAuditLogs :many
-- Newest first, keyset-paged on (occurred_at, id): unique even when several
-- events share a timestamp, so no row is skipped or repeated.
SELECT id, event_name, aggregate_id, actor_type, actor_id, payload, occurred_at, recorded_at
FROM audit_logs
WHERE (sqlc.narg(aggregate_id)::text IS NULL OR aggregate_id = sqlc.narg(aggregate_id)::text)
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
       OR (occurred_at, id) < (sqlc.narg(cursor_at)::timestamptz, sqlc.narg(cursor_id)::uuid))
ORDER BY occurred_at DESC, id DESC
LIMIT sqlc.arg(row_limit);
