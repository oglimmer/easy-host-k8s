CREATE TABLE IF NOT EXISTS content (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    slug VARCHAR(255) NOT NULL UNIQUE,
    owner VARCHAR(255) NOT NULL,
    title VARCHAR(255) NULL,
    source_url VARCHAR(2048) NULL,
    creator VARCHAR(255) NOT NULL DEFAULT 'admin',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    INDEX idx_content_owner (owner),
    INDEX idx_content_slug (slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS content_file (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    content_id BIGINT NOT NULL,
    file_path VARCHAR(512) NOT NULL,
    file_data LONGBLOB NOT NULL,
    content_type VARCHAR(255) NOT NULL,
    CONSTRAINT fk_content_file_content FOREIGN KEY (content_id) REFERENCES content(id) ON DELETE CASCADE,
    INDEX idx_content_file_lookup (content_id, file_path)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
