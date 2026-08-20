-- Note: this drops the wrapped keys, permanently orphaning the ciphertexts of
-- any encrypted content. Decrypt (remove encryption from) that content first.
ALTER TABLE content
    DROP COLUMN enc_kdf,
    DROP COLUMN enc_salt,
    DROP COLUMN enc_wrapped_key;
