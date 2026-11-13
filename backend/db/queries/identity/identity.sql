-- name: GetAdminByUsername :one
SELECT id, username, password_hash FROM admins WHERE username = $1;

-- name: GetAdmin :one
SELECT id, username, password_hash FROM admins WHERE id = $1;

-- name: GetAnyAdmin :one
SELECT id, username, password_hash FROM admins LIMIT 1;

-- name: InsertAdmin :exec
INSERT INTO admins (id, username, password_hash) VALUES ($1, $2, $3);

-- name: UpdateAdminPassword :exec
UPDATE admins SET password_hash = $2, updated_at = now() WHERE id = $1;

-- name: InsertTenantAccount :exec
INSERT INTO tenant_accounts (id, username, password_hash, full_name, phone, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7);

-- name: GetTenantAccount :one
SELECT * FROM tenant_accounts WHERE id = $1;

-- name: GetTenantAccountForUpdate :one
SELECT * FROM tenant_accounts WHERE id = $1 FOR UPDATE;

-- name: GetTenantAccountByUsername :one
SELECT * FROM tenant_accounts WHERE username = $1;

-- name: ListTenantAccounts :many
SELECT * FROM tenant_accounts
WHERE sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text
ORDER BY created_at DESC;

-- name: ListActiveTenantAccounts :many
SELECT * FROM tenant_accounts WHERE status = 'ACTIVE';

-- name: UpdateTenantAccount :exec
UPDATE tenant_accounts
SET status = $2, room_id = $3, unassigned_at = $4, password_hash = $5, updated_at = now()
WHERE id = $1;

-- name: InsertSession :exec
INSERT INTO sessions (token_hash, subject_type, subject_id, expires_at) VALUES ($1, $2, $3, $4);

-- name: GetSession :one
SELECT token_hash, subject_type, subject_id, expires_at FROM sessions WHERE token_hash = $1;

-- name: UpdateSessionExpiry :exec
UPDATE sessions SET expires_at = $2 WHERE token_hash = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteSessionsForSubject :exec
DELETE FROM sessions WHERE subject_type = $1 AND subject_id = $2;
