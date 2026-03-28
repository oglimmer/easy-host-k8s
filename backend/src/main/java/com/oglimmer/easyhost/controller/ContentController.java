package com.oglimmer.easyhost.controller;

import com.oglimmer.easyhost.dto.ContentResponse;
import com.oglimmer.easyhost.service.ContentService;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.security.core.annotation.AuthenticationPrincipal;
import org.springframework.security.core.userdetails.UserDetails;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.multipart.MultipartFile;

import java.io.IOException;
import java.util.List;
import java.util.Map;

@RestController
@RequestMapping("/api/content")
@RequiredArgsConstructor
@Slf4j
public class ContentController {

    private final ContentService contentService;

    @GetMapping
    public List<ContentResponse> list(@AuthenticationPrincipal UserDetails user) {
        return contentService.listByOwner(user.getUsername());
    }

    @GetMapping("/{slug}")
    public ContentResponse get(@PathVariable String slug,
                               @AuthenticationPrincipal UserDetails user) {
        return contentService.getBySlug(slug, user.getUsername());
    }

    @PostMapping
    public ResponseEntity<ContentResponse> create(@RequestParam String slug,
                                                  @RequestParam MultipartFile file,
                                                  @RequestParam(required = false) String title,
                                                  @RequestParam(required = false) String sourceUrl,
                                                  @AuthenticationPrincipal UserDetails user) throws IOException {
        ContentResponse response = contentService.create(slug, file, user.getUsername(), title, sourceUrl);
        return ResponseEntity.status(HttpStatus.CREATED).body(response);
    }

    @PutMapping("/{slug}")
    public ContentResponse update(@PathVariable String slug,
                                  @RequestParam(required = false) MultipartFile file,
                                  @RequestParam(required = false) String title,
                                  @RequestParam(required = false) String sourceUrl,
                                  @AuthenticationPrincipal UserDetails user) throws IOException {
        return contentService.update(slug, file, user.getUsername(), title, sourceUrl);
    }

    @DeleteMapping("/{slug}")
    public ResponseEntity<Void> delete(@PathVariable String slug,
                                       @AuthenticationPrincipal UserDetails user) {
        contentService.delete(slug, user.getUsername());
        return ResponseEntity.noContent().build();
    }

    @ExceptionHandler(ContentService.ContentNotFoundException.class)
    public ResponseEntity<Map<String, String>> handleNotFound(ContentService.ContentNotFoundException e) {
        return ResponseEntity.status(HttpStatus.NOT_FOUND)
                .body(Map.of("error", e.getMessage()));
    }

    @ExceptionHandler(ContentService.SlugAlreadyExistsException.class)
    public ResponseEntity<Map<String, String>> handleConflict(ContentService.SlugAlreadyExistsException e) {
        return ResponseEntity.status(HttpStatus.CONFLICT)
                .body(Map.of("error", e.getMessage()));
    }

    @ExceptionHandler(ContentService.InvalidSlugException.class)
    public ResponseEntity<Map<String, String>> handleInvalidSlug(ContentService.InvalidSlugException e) {
        return ResponseEntity.status(HttpStatus.BAD_REQUEST)
                .body(Map.of("error", e.getMessage()));
    }
}
