package com.oglimmer.easyhost.repository;

import com.oglimmer.easyhost.model.ContentFile;
import org.springframework.data.jpa.repository.JpaRepository;

import java.util.Optional;

public interface ContentFileRepository extends JpaRepository<ContentFile, Long> {

    Optional<ContentFile> findByContentSlugAndFilePath(String slug, String filePath);
}
