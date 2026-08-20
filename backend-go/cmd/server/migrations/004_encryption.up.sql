-- At-rest encryption for passphrase-protected content.
--
-- Envelope encryption: the passphrase derives a key-encryption key (Argon2id,
-- salt below), which wraps a random per-content data-encryption key. Only the
-- wrapped key is stored, so neither the passphrase nor a usable key is
-- recoverable from the database. See internal/crypto for the scheme.
--
-- A NULL enc_kdf marks the content as unencrypted; all three columns are set or
-- all three are NULL together.

ALTER TABLE content
    ADD COLUMN enc_kdf         VARCHAR(128)   NULL COMMENT 'Argon2id parameters, e.g. argon2id$v=19$m=65536,t=3,p=4; NULL = not encrypted',
    ADD COLUMN enc_salt        VARBINARY(32)  NULL COMMENT 'per-content Argon2id salt',
    ADD COLUMN enc_wrapped_key VARBINARY(128) NULL COMMENT 'data-encryption key sealed under the passphrase-derived key (nonce || ciphertext || tag)';

-- File payloads of an encrypted entry are stored as nonce || ciphertext || tag
-- in the existing file_data column; no schema change is needed there.
