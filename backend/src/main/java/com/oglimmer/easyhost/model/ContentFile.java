package com.oglimmer.easyhost.model;

import jakarta.persistence.*;
import lombok.*;

@Entity
@Table(name = "content_file")
@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class ContentFile {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    @ManyToOne(fetch = FetchType.LAZY)
    @JoinColumn(name = "content_id", nullable = false)
    @ToString.Exclude
    @EqualsAndHashCode.Exclude
    private Content content;

    @Column(name = "file_path", nullable = false)
    private String filePath;

    @Lob
    @Column(name = "file_data", nullable = false, columnDefinition = "LONGBLOB")
    private byte[] fileData;

    @Column(name = "content_type", nullable = false)
    private String contentType;
}
