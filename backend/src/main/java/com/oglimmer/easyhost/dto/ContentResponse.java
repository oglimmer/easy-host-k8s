package com.oglimmer.easyhost.dto;

import lombok.Builder;
import lombok.Data;

import java.time.LocalDateTime;
import java.util.List;

@Data
@Builder
public class ContentResponse {
    private Long id;
    private String slug;
    private String title;
    private String sourceUrl;
    private String owner;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;
    private List<String> files;
}
