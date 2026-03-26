package com.oglimmer.easyhost.controller;

import com.oglimmer.easyhost.service.ContentService;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Controller;
import org.springframework.ui.Model;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.multipart.MultipartFile;
import org.springframework.web.servlet.mvc.support.RedirectAttributes;

import java.io.IOException;
import java.security.Principal;

@Controller
@RequiredArgsConstructor
@Slf4j
public class WebController {

    private final ContentService contentService;

    @GetMapping("/login")
    public String login() {
        return "login";
    }

    @GetMapping({"/", "/dashboard"})
    public String dashboard(Principal principal, Model model) {
        if (principal == null) {
            return "redirect:/dashboard";
        }
        model.addAttribute("contents", contentService.listByOwner(principal.getName()));
        return "dashboard";
    }

    @GetMapping("/upload")
    public String uploadForm() {
        return "upload";
    }

    @PostMapping("/upload")
    public String upload(@RequestParam String slug,
                         @RequestParam MultipartFile file,
                         Principal principal,
                         RedirectAttributes redirectAttributes) {
        try {
            contentService.create(slug, file, principal.getName());
            redirectAttributes.addFlashAttribute("success", "Content '" + slug + "' created successfully.");
        } catch (ContentService.SlugAlreadyExistsException e) {
            redirectAttributes.addFlashAttribute("error", "Slug '" + slug + "' already exists.");
            return "redirect:/upload";
        } catch (ContentService.InvalidSlugException e) {
            redirectAttributes.addFlashAttribute("error", e.getMessage());
            return "redirect:/upload";
        } catch (IOException e) {
            log.error("Failed to upload content", e);
            redirectAttributes.addFlashAttribute("error", "Upload failed: " + e.getMessage());
            return "redirect:/upload";
        }
        return "redirect:/dashboard";
    }

    @GetMapping("/edit/{slug}")
    public String editForm(@PathVariable String slug,
                           Principal principal,
                           Model model,
                           RedirectAttributes redirectAttributes) {
        try {
            model.addAttribute("content", contentService.getBySlug(slug, principal.getName()));
            return "edit";
        } catch (ContentService.ContentNotFoundException e) {
            redirectAttributes.addFlashAttribute("error", "Content not found.");
            return "redirect:/dashboard";
        }
    }

    @PostMapping("/edit/{slug}")
    public String edit(@PathVariable String slug,
                       @RequestParam MultipartFile file,
                       Principal principal,
                       RedirectAttributes redirectAttributes) {
        try {
            contentService.update(slug, file, principal.getName());
            redirectAttributes.addFlashAttribute("success", "Content '" + slug + "' updated successfully.");
        } catch (ContentService.ContentNotFoundException e) {
            redirectAttributes.addFlashAttribute("error", "Content not found.");
        } catch (IOException e) {
            log.error("Failed to update content", e);
            redirectAttributes.addFlashAttribute("error", "Update failed: " + e.getMessage());
            return "redirect:/edit/" + slug;
        }
        return "redirect:/dashboard";
    }

    @PostMapping("/delete/{slug}")
    public String delete(@PathVariable String slug,
                         Principal principal,
                         RedirectAttributes redirectAttributes) {
        try {
            contentService.delete(slug, principal.getName());
            redirectAttributes.addFlashAttribute("success", "Content '" + slug + "' deleted.");
        } catch (ContentService.ContentNotFoundException e) {
            redirectAttributes.addFlashAttribute("error", "Content not found.");
        }
        return "redirect:/dashboard";
    }
}
