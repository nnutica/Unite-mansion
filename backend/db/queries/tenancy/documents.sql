-- name: GetLatestConsentText :one
SELECT * FROM consent_texts ORDER BY version DESC LIMIT 1;

-- name: InsertConsentText :exec
INSERT INTO consent_texts (version, body, created_at) VALUES ($1, $2, $3);

-- name: GetConsent :one
SELECT accepted_at FROM tenant_consents WHERE account_id = $1 AND version = $2;

-- name: InsertConsent :exec
INSERT INTO tenant_consents (account_id, version, accepted_at) VALUES ($1, $2, $3)
ON CONFLICT (account_id, version) DO NOTHING;

-- name: InsertDocument :exec
INSERT INTO tenant_documents (id, account_id, room_id, document_type, file_key, content_type, size_bytes, uploaded_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListDocumentsByAccount :many
SELECT * FROM tenant_documents WHERE account_id = $1 ORDER BY uploaded_at DESC;

-- name: ListDocumentsByRoom :many
SELECT * FROM tenant_documents WHERE room_id = $1 ORDER BY uploaded_at DESC;

-- name: SetDocumentsPurgeAt :exec
UPDATE tenant_documents SET purge_at = sqlc.narg(purge_at) WHERE account_id = $1;

-- name: ListDocumentsDueForPurge :many
SELECT * FROM tenant_documents WHERE purge_at IS NOT NULL AND purge_at <= $1 ORDER BY account_id, uploaded_at;

-- name: DeleteDocument :exec
DELETE FROM tenant_documents WHERE id = $1;
