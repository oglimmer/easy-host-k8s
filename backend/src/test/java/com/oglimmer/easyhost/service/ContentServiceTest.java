package com.oglimmer.easyhost.service;

import com.oglimmer.easyhost.dto.ContentResponse;
import com.oglimmer.easyhost.model.Content;
import com.oglimmer.easyhost.model.ContentFile;
import com.oglimmer.easyhost.repository.ContentFileRepository;
import com.oglimmer.easyhost.repository.ContentRepository;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.mock.web.MockMultipartFile;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.util.ArrayList;
import java.util.List;
import java.util.Optional;
import java.util.zip.ZipEntry;
import java.util.zip.ZipOutputStream;

import static org.assertj.core.api.Assertions.*;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
class ContentServiceTest {

    @Mock
    private ContentRepository contentRepository;

    @Mock
    private ContentFileRepository contentFileRepository;

    @InjectMocks
    private ContentService contentService;

    @Test
    void listByOwner_returnsContentForOwner() {
        Content content = buildContent("test-slug", "owner1");
        when(contentRepository.findByOwner("owner1")).thenReturn(List.of(content));

        List<ContentResponse> result = contentService.listByOwner("owner1");

        assertThat(result).hasSize(1);
        assertThat(result.get(0).getSlug()).isEqualTo("test-slug");
    }

    @Test
    void getBySlug_returnsContent() {
        Content content = buildContent("my-slug", "owner1");
        when(contentRepository.findBySlug("my-slug")).thenReturn(Optional.of(content));

        ContentResponse result = contentService.getBySlug("my-slug", "owner1");

        assertThat(result.getSlug()).isEqualTo("my-slug");
    }

    @Test
    void getBySlug_throwsWhenNotFound() {
        when(contentRepository.findBySlug("missing")).thenReturn(Optional.empty());

        assertThatThrownBy(() -> contentService.getBySlug("missing", "owner1"))
                .isInstanceOf(ContentService.ContentNotFoundException.class);
    }

    @Test
    void getBySlug_throwsWhenWrongOwner() {
        Content content = buildContent("my-slug", "owner1");
        when(contentRepository.findBySlug("my-slug")).thenReturn(Optional.of(content));

        assertThatThrownBy(() -> contentService.getBySlug("my-slug", "other-owner"))
                .isInstanceOf(ContentService.ContentNotFoundException.class);
    }

    @Test
    void create_singleHtmlFile() throws IOException {
        MockMultipartFile file = new MockMultipartFile("file", "page.html", "text/html", "<h1>Hi</h1>".getBytes());
        when(contentRepository.existsBySlug("hello")).thenReturn(false);
        Content saved = buildContent("hello", "owner1");
        when(contentRepository.save(any())).thenReturn(saved);
        when(contentRepository.findBySlug("hello")).thenReturn(Optional.of(saved));

        ContentResponse result = contentService.create("hello", file, "owner1", null, null);

        assertThat(result.getSlug()).isEqualTo("hello");
        verify(contentRepository).save(any());
    }

    @Test
    void create_throwsOnDuplicateSlug() {
        when(contentRepository.existsBySlug("taken")).thenReturn(true);

        assertThatThrownBy(() -> contentService.create("taken", null, "owner1", null, null))
                .isInstanceOf(ContentService.SlugAlreadyExistsException.class);
    }

    @Test
    void create_zipFileExtractsEntries() throws IOException {
        byte[] zipData = createZip("index.html", "<h1>Hi</h1>", "style.css", "body{}");
        MockMultipartFile file = new MockMultipartFile("file", "site.zip", "application/zip", zipData);
        when(contentRepository.existsBySlug("ziptest")).thenReturn(false);
        Content saved = buildContent("ziptest", "owner1");
        when(contentRepository.save(any())).thenReturn(saved);
        when(contentRepository.findBySlug("ziptest")).thenReturn(Optional.of(saved));

        contentService.create("ziptest", file, "owner1", null, null);

        assertThat(saved.getFiles()).hasSize(2);
        assertThat(saved.getFiles().stream().map(ContentFile::getFilePath))
                .containsExactlyInAnyOrder("index.html", "style.css");
    }

    @Test
    void create_zipSkipsHiddenAndMacosx() throws IOException {
        byte[] zipData = createZip(".hidden", "x", "__MACOSX/foo", "y", "real.html", "<ok/>");
        MockMultipartFile file = new MockMultipartFile("file", "site.zip", "application/zip", zipData);
        when(contentRepository.existsBySlug("skiptest")).thenReturn(false);
        Content saved = buildContent("skiptest", "owner1");
        when(contentRepository.save(any())).thenReturn(saved);
        when(contentRepository.findBySlug("skiptest")).thenReturn(Optional.of(saved));

        contentService.create("skiptest", file, "owner1", null, null);

        assertThat(saved.getFiles()).hasSize(1);
        assertThat(saved.getFiles().get(0).getFilePath()).isEqualTo("real.html");
    }

    @Test
    void create_zipRejectsPathTraversal() throws IOException {
        byte[] zipData = createZip("../../etc/passwd", "evil");
        MockMultipartFile file = new MockMultipartFile("file", "evil.zip", "application/zip", zipData);
        when(contentRepository.existsBySlug("evil")).thenReturn(false);
        Content saved = buildContent("evil", "owner1");
        when(contentRepository.save(any())).thenReturn(saved);

        assertThatThrownBy(() -> contentService.create("evil", file, "owner1", null, null))
                .isInstanceOf(ContentService.InvalidFilePathException.class);
    }

    @Test
    void delete_removesContent() {
        Content content = buildContent("del-me", "owner1");
        when(contentRepository.findBySlug("del-me")).thenReturn(Optional.of(content));

        contentService.delete("del-me", "owner1");

        verify(contentRepository).delete(content);
    }

    @Test
    void delete_throwsWhenWrongOwner() {
        Content content = buildContent("del-me", "owner1");
        when(contentRepository.findBySlug("del-me")).thenReturn(Optional.of(content));

        assertThatThrownBy(() -> contentService.delete("del-me", "other"))
                .isInstanceOf(ContentService.ContentNotFoundException.class);
    }

    @Test
    void getFile_returnsOptional() {
        ContentFile cf = ContentFile.builder().filePath("index.html").build();
        when(contentFileRepository.findByContentSlugAndFilePath("s", "index.html"))
                .thenReturn(Optional.of(cf));

        Optional<ContentFile> result = contentService.getFile("s", "index.html");

        assertThat(result).isPresent();
        assertThat(result.get().getFilePath()).isEqualTo("index.html");
    }

    @Test
    void getFile_returnsEmptyWhenNotFound() {
        when(contentFileRepository.findByContentSlugAndFilePath("s", "nope.html"))
                .thenReturn(Optional.empty());

        assertThat(contentService.getFile("s", "nope.html")).isEmpty();
    }

    @ParameterizedTest
    @ValueSource(strings = {"a", "ab", "a1", "my-site", "test-123-site"})
    void validateSlug_acceptsValidSlugs(String slug) throws IOException {
        when(contentRepository.existsBySlug(slug)).thenReturn(false);
        Content saved = buildContent(slug, "owner");
        when(contentRepository.save(any())).thenReturn(saved);
        when(contentRepository.findBySlug(slug)).thenReturn(Optional.of(saved));
        MockMultipartFile file = new MockMultipartFile("file", "p.html", "text/html", "<h1/>".getBytes());

        assertThatCode(() -> contentService.create(slug, file, "owner", null, null)).doesNotThrowAnyException();
    }

    @ParameterizedTest
    @ValueSource(strings = {"-bad", "bad-", "BAD", "no spaces", "no_underscores", "no.dots"})
    void validateSlug_rejectsInvalidSlugs(String slug) {
        when(contentRepository.existsBySlug(slug)).thenReturn(false);
        MockMultipartFile file = new MockMultipartFile("file", "p.html", "text/html", "<h1/>".getBytes());

        assertThatThrownBy(() -> contentService.create(slug, file, "owner", null, null))
                .isInstanceOf(ContentService.InvalidSlugException.class);
    }

    @Test
    void normalizeFilePath_rejectsTraversal() {
        assertThatThrownBy(() -> contentService.normalizeFilePath("../../etc/passwd"))
                .isInstanceOf(ContentService.InvalidFilePathException.class);
    }

    @Test
    void normalizeFilePath_acceptsNormalPaths() {
        assertThat(contentService.normalizeFilePath("css/style.css")).isEqualTo("css/style.css");
        assertThat(contentService.normalizeFilePath("index.html")).isEqualTo("index.html");
    }

    private Content buildContent(String slug, String owner) {
        return Content.builder()
                .id(1L)
                .slug(slug)
                .owner(owner)
                .files(new ArrayList<>())
                .build();
    }

    private byte[] createZip(String... nameContentPairs) throws IOException {
        ByteArrayOutputStream baos = new ByteArrayOutputStream();
        try (ZipOutputStream zos = new ZipOutputStream(baos)) {
            for (int i = 0; i < nameContentPairs.length; i += 2) {
                zos.putNextEntry(new ZipEntry(nameContentPairs[i]));
                zos.write(nameContentPairs[i + 1].getBytes());
                zos.closeEntry();
            }
        }
        return baos.toByteArray();
    }
}
