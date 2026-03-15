package com.oglimmer.easyhost.controller;

import com.oglimmer.easyhost.model.ContentFile;
import com.oglimmer.easyhost.service.ContentService;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/s")
@RequiredArgsConstructor
@Slf4j
public class ServingController {

    private final ContentService contentService;

    @GetMapping("/{slug}")
    public ResponseEntity<byte[]> serveIndex(@PathVariable String slug) {
        return serveFile(slug, "index.html");
    }

    @GetMapping("/{slug}/{*filePath}")
    public ResponseEntity<byte[]> serveFile(@PathVariable String slug,
                                            @PathVariable String filePath) {
        if (filePath.startsWith("/")) {
            filePath = filePath.substring(1);
        }
        if (filePath.contains("..")) {
            return ResponseEntity.badRequest().build();
        }

        return contentService.getFile(slug, filePath)
                .map(file -> {
                    HttpHeaders headers = new HttpHeaders();
                    headers.set(HttpHeaders.CONTENT_TYPE, file.getContentType());
                    headers.set(HttpHeaders.CACHE_CONTROL, "public, max-age=3600");
                    return new ResponseEntity<>(file.getFileData(), headers, HttpStatus.OK);
                })
                .orElseGet(() -> ResponseEntity.notFound().build());
    }
}
