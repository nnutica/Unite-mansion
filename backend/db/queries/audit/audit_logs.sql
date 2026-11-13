-- name: InsertAuditLog :exec
INSERT INTO audit_logs (event_name, aggregate_id, actor_type, actor_id, payload, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListAuditLogsByAggregate :many
SELECT id, event_name, aggregate_id, actor_type, actor_id, payload, occurred_at, recorded_at
FROM audit_logs
WHERE aggregate_id = $1
ORDER BY occurred_at, recorded_at;
