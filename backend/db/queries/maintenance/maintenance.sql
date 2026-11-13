-- name: InsertRequest :exec
INSERT INTO maintenance_requests (id, room_id, category, description, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: InsertImage :exec
INSERT INTO maintenance_images (request_id, position, file_key) VALUES ($1, $2, $3);

-- name: UpdateRequestStatus :exec
UPDATE maintenance_requests SET status = $2, updated_at = $3 WHERE id = $1;

-- name: GetRequest :one
SELECT * FROM maintenance_requests WHERE id = $1;

-- name: GetRequestForUpdate :one
SELECT * FROM maintenance_requests WHERE id = $1 FOR UPDATE;

-- name: ListRequestsByRoom :many
SELECT * FROM maintenance_requests WHERE room_id = $1 ORDER BY created_at DESC;

-- name: ListRequests :many
SELECT * FROM maintenance_requests
WHERE sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text
ORDER BY created_at DESC;

-- name: ListImages :many
SELECT request_id, file_key FROM maintenance_images WHERE request_id = ANY(sqlc.arg(request_ids)::uuid[]) ORDER BY request_id, position;
