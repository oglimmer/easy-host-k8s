package com.oglimmer.easyhost.repository;

import com.oglimmer.easyhost.model.ContentFile;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

import java.util.List;
import java.util.Optional;

public interface ContentFileRepository extends JpaRepository<ContentFile, Long> {

    Optional<ContentFile> findByContentSlugAndFilePath(String slug, String filePath);

    @Query("SELECT cf.filePath FROM ContentFile cf WHERE cf.content.id = :contentId")
    List<String> findFilePathsByContentId(@Param("contentId") Long contentId);
}
