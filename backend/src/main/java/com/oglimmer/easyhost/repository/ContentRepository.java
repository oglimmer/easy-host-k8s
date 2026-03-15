package com.oglimmer.easyhost.repository;

import com.oglimmer.easyhost.model.Content;
import org.springframework.data.jpa.repository.JpaRepository;

import java.util.List;
import java.util.Optional;

public interface ContentRepository extends JpaRepository<Content, Long> {

    Optional<Content> findBySlug(String slug);

    List<Content> findByOwner(String owner);

    boolean existsBySlug(String slug);
}
