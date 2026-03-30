package com.oglimmer.easyhost.controller;

import com.oglimmer.easyhost.model.Content;
import com.oglimmer.easyhost.model.ContentFile;
import com.oglimmer.easyhost.repository.ContentRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.web.servlet.MockMvc;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;

@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("test")
class ServingControllerIT {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private ContentRepository contentRepository;

    @BeforeEach
    void setUp() {
        contentRepository.deleteAll();
    }

    @Test
    void serveIndex_returnsHtml() throws Exception {
        createContent("hello", "index.html", "<h1>Hello</h1>", "text/html");

        mockMvc.perform(get("/s/hello"))
                .andExpect(status().isOk())
                .andExpect(header().string("Content-Type", "text/html"))
                .andExpect(header().string("Cache-Control", "public, max-age=3600"))
                .andExpect(content().string("<h1>Hello</h1>"));
    }

    @Test
    void serveFile_returnsSubpath() throws Exception {
        Content content = createContent("site", "index.html", "<h1>Home</h1>", "text/html");
        addFile(content, "css/style.css", "body{}", "text/css");

        mockMvc.perform(get("/s/site/css/style.css"))
                .andExpect(status().isOk())
                .andExpect(header().string("Content-Type", "text/css"))
                .andExpect(content().string("body{}"));
    }

    @Test
    void serveFile_returns404ForMissing() throws Exception {
        createContent("hello", "index.html", "<h1>Hello</h1>", "text/html");

        mockMvc.perform(get("/s/hello/missing.js"))
                .andExpect(status().isNotFound());
    }

    @Test
    void serveFile_returns404ForMissingSlug() throws Exception {
        mockMvc.perform(get("/s/nonexistent"))
                .andExpect(status().isNotFound());
    }

    @Test
    void serveFile_rejectsPathTraversal() throws Exception {
        createContent("hello", "index.html", "<h1>Hello</h1>", "text/html");

        mockMvc.perform(get("/s/hello/../../secret"))
                .andExpect(status().isBadRequest());
    }

    @Test
    void serveFile_hasCspHeader() throws Exception {
        createContent("csp", "index.html", "<h1>CSP</h1>", "text/html");

        mockMvc.perform(get("/s/csp"))
                .andExpect(status().isOk())
                .andExpect(header().exists("Content-Security-Policy"));
    }

    private Content createContent(String slug, String filePath, String data, String contentType) {
        Content content = Content.builder()
                .slug(slug)
                .owner("testowner")
                .creator("testowner")
                .build();
        ContentFile file = ContentFile.builder()
                .content(content)
                .filePath(filePath)
                .fileData(data.getBytes())
                .contentType(contentType)
                .build();
        content.getFiles().add(file);
        return contentRepository.save(content);
    }

    private void addFile(Content content, String filePath, String data, String contentType) {
        ContentFile file = ContentFile.builder()
                .content(content)
                .filePath(filePath)
                .fileData(data.getBytes())
                .contentType(contentType)
                .build();
        content.getFiles().add(file);
        contentRepository.save(content);
    }
}
