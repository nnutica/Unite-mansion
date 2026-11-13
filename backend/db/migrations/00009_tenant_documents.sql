-- +goose Up
-- PDPA consent text, versioned: every edit is a new version that tenants
-- must accept again before uploading documents.
CREATE TABLE consent_texts (
    version    int         PRIMARY KEY,
    body       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tenant_consents (
    account_id  uuid        NOT NULL REFERENCES tenant_accounts (id),
    version     int         NOT NULL REFERENCES consent_texts (version),
    accepted_at timestamptz NOT NULL,
    PRIMARY KEY (account_id, version)
);

CREATE TABLE tenant_documents (
    id            uuid        PRIMARY KEY,
    account_id    uuid        NOT NULL REFERENCES tenant_accounts (id),
    room_id       uuid        NOT NULL REFERENCES rooms (id),
    document_type text        NOT NULL CHECK (document_type IN ('ID_CARD_COPY', 'LEASE_CONTRACT')),
    file_key      text        NOT NULL UNIQUE,
    content_type  text        NOT NULL,
    size_bytes    bigint      NOT NULL CHECK (size_bytes > 0),
    uploaded_at   timestamptz NOT NULL,
    -- §6.6 retention: set 30 days after the account is unbound, cleared if re-bound.
    purge_at      timestamptz
);
CREATE INDEX tenant_documents_account_idx ON tenant_documents (account_id);
CREATE INDEX tenant_documents_room_idx ON tenant_documents (room_id);
CREATE INDEX tenant_documents_purge_idx ON tenant_documents (purge_at) WHERE purge_at IS NOT NULL;

-- Version 1, written from what context.md states. Review with the owner
-- before real use; publish changes as a new version from P-A13.
INSERT INTO consent_texts (version, body) VALUES (1,
'หอพัก B Mansion ขอเก็บสำเนาบัตรประชาชนและสัญญาเช่าของท่าน เพื่อใช้ยืนยันตัวตนและประกอบการเช่าห้องพักเท่านั้น
เอกสารจะถูกเก็บแบบส่วนตัว มีเพียงเจ้าของหอพักที่เปิดดูได้
เมื่อท่านย้ายออกและเจ้าของหอพักยกเลิกการผูกบัญชีกับห้อง ระบบจะลบเอกสารของท่านภายใน 30 วัน
ข้อมูลบิลและหลักฐานการชำระเงินจะถูกเก็บต่อไปเพื่อเป็นหลักฐานทางการเงิน');

-- +goose Down
DROP TABLE tenant_documents;
DROP TABLE tenant_consents;
DROP TABLE consent_texts;
