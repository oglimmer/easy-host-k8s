package com.oglimmer.easyhost.service;

import com.oglimmer.easyhost.dto.ContentResponse;
import com.oglimmer.easyhost.model.Content;
import com.oglimmer.easyhost.model.ContentFile;
import com.oglimmer.easyhost.repository.ContentFileRepository;
import com.oglimmer.easyhost.repository.ContentRepository;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.MediaType;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.multipart.MultipartFile;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.util.List;
import java.util.Optional;
import java.util.zip.ZipEntry;
import java.util.zip.ZipInputStream;

@Service
@RequiredArgsConstructor
@Slf4j
public class ContentService {

    private final ContentRepository contentRepository;
    private final ContentFileRepository contentFileRepository;

    @Transactional(readOnly = true)
    public List<ContentResponse> listByOwner(String owner) {
        return contentRepository.findByOwner(owner).stream()
                .map(this::toResponse)
                .toList();
    }

    @Transactional(readOnly = true)
    public ContentResponse getBySlug(String slug, String owner) {
        Content content = contentRepository.findBySlug(slug)
                .orElseThrow(() -> new ContentNotFoundException(slug));
        if (!content.getOwner().equals(owner)) {
            throw new ContentNotFoundException(slug);
        }
        return toResponse(content);
    }

    @Transactional
    public ContentResponse create(String slug, MultipartFile file, String owner, String title, String sourceUrl) throws IOException {
        if (contentRepository.existsBySlug(slug)) {
            throw new SlugAlreadyExistsException(slug);
        }
        validateSlug(slug);

        Content content = Content.builder()
                .slug(slug)
                .owner(owner)
                .title(title != null && !title.isBlank() ? title.strip() : slug)
                .sourceUrl(sourceUrl != null && !sourceUrl.isBlank() ? sourceUrl.strip() : null)
                .build();
        content = contentRepository.save(content);

        addFiles(content, file);

        return toResponse(contentRepository.findBySlug(slug).orElseThrow());
    }

    @Transactional
    public ContentResponse update(String slug, MultipartFile file, String owner, String title, String sourceUrl) throws IOException {
        Content content = contentRepository.findBySlug(slug)
                .orElseThrow(() -> new ContentNotFoundException(slug));
        if (!content.getOwner().equals(owner)) {
            throw new ContentNotFoundException(slug);
        }

        if (title != null) {
            content.setTitle(!title.isBlank() ? title.strip() : slug);
        }
        if (sourceUrl != null) {
            content.setSourceUrl(!sourceUrl.isBlank() ? sourceUrl.strip() : null);
        }

        if (file != null && !file.isEmpty()) {
            content.getFiles().clear();
            contentRepository.flush();
            addFiles(content, file);
        }

        return toResponse(contentRepository.findBySlug(slug).orElseThrow());
    }

    @Transactional
    public void delete(String slug, String owner) {
        Content content = contentRepository.findBySlug(slug)
                .orElseThrow(() -> new ContentNotFoundException(slug));
        if (!content.getOwner().equals(owner)) {
            throw new ContentNotFoundException(slug);
        }
        contentRepository.delete(content);
    }

    @Transactional(readOnly = true)
    public Optional<ContentFile> getFile(String slug, String filePath) {
        return contentFileRepository.findByContentSlugAndFilePath(slug, filePath);
    }

    private void addFiles(Content content, MultipartFile file) throws IOException {
        String originalFilename = file.getOriginalFilename();
        if (originalFilename != null && originalFilename.toLowerCase().endsWith(".zip")) {
            extractZip(content, file.getBytes());
        } else {
            ContentFile contentFile = ContentFile.builder()
                    .content(content)
                    .filePath("index.html")
                    .fileData(file.getBytes())
                    .contentType(MediaType.TEXT_HTML_VALUE)
                    .build();
            content.getFiles().add(contentFile);
        }
    }

    private void extractZip(Content content, byte[] zipData) throws IOException {
        try (ZipInputStream zis = new ZipInputStream(new ByteArrayInputStream(zipData))) {
            ZipEntry entry;
            while ((entry = zis.getNextEntry()) != null) {
                if (entry.isDirectory()) {
                    continue;
                }
                String name = normalizeFilePath(entry.getName());
                // Skip hidden files and __MACOSX
                if (name.startsWith(".") || name.startsWith("__MACOSX")) {
                    continue;
                }
                byte[] data = zis.readAllBytes();
                String contentType = guessContentType(name);

                ContentFile contentFile = ContentFile.builder()
                        .content(content)
                        .filePath(name)
                        .fileData(data)
                        .contentType(contentType)
                        .build();
                content.getFiles().add(contentFile);
            }
        }
    }

    String normalizeFilePath(String path) {
        // Reject absolute paths and path traversal sequences (Zip Slip prevention)
        String normalized = java.nio.file.Path.of(path).normalize().toString();
        if (normalized.startsWith("..") || normalized.startsWith("/") || normalized.startsWith("\\")) {
            throw new InvalidFilePathException(path);
        }
        return normalized;
    }

    private String guessContentType(String filename) {
        String lower = filename.toLowerCase();
        if (lower.endsWith(".html") || lower.endsWith(".htm")) return MediaType.TEXT_HTML_VALUE;
        if (lower.endsWith(".css")) return "text/css";
        if (lower.endsWith(".js")) return "application/javascript";
        if (lower.endsWith(".json")) return MediaType.APPLICATION_JSON_VALUE;
        if (lower.endsWith(".png")) return MediaType.IMAGE_PNG_VALUE;
        if (lower.endsWith(".jpg") || lower.endsWith(".jpeg")) return MediaType.IMAGE_JPEG_VALUE;
        if (lower.endsWith(".gif")) return MediaType.IMAGE_GIF_VALUE;
        if (lower.endsWith(".svg")) return "image/svg+xml";
        if (lower.endsWith(".ico")) return "image/x-icon";
        if (lower.endsWith(".woff")) return "font/woff";
        if (lower.endsWith(".woff2")) return "font/woff2";
        if (lower.endsWith(".ttf")) return "font/ttf";
        return MediaType.APPLICATION_OCTET_STREAM_VALUE;
    }

    private void validateSlug(String slug) {
        if (!slug.matches("^[a-z0-9][a-z0-9-]*[a-z0-9]$") && !slug.matches("^[a-z0-9]$")) {
            throw new InvalidSlugException(slug);
        }
    }

    private ContentResponse toResponse(Content content) {
        return ContentResponse.builder()
                .id(content.getId())
                .slug(content.getSlug())
                .title(content.getTitle())
                .sourceUrl(content.getSourceUrl())
                .owner(content.getOwner())
                .createdAt(content.getCreatedAt())
                .updatedAt(content.getUpdatedAt())
                .files(content.getFiles().stream()
                        .map(ContentFile::getFilePath)
                        .toList())
                .build();
    }

    public static class ContentNotFoundException extends RuntimeException {
        public ContentNotFoundException(String slug) {
            super("Content not found: " + slug);
        }
    }

    public static class SlugAlreadyExistsException extends RuntimeException {
        public SlugAlreadyExistsException(String slug) {
            super("Slug already exists: " + slug);
        }
    }

    public static class InvalidSlugException extends RuntimeException {
        public InvalidSlugException(String slug) {
            super("Invalid slug: " + slug + ". Must contain only lowercase letters, numbers, and hyphens.");
        }
    }

    public static class InvalidFilePathException extends RuntimeException {
        public InvalidFilePathException(String path) {
            super("Invalid file path in archive: " + path);
        }
    }
}
