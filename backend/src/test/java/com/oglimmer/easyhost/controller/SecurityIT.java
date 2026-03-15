package com.oglimmer.easyhost.controller;

import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.web.servlet.MockMvc;

import static org.springframework.security.test.web.servlet.request.SecurityMockMvcRequestPostProcessors.httpBasic;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("test")
class SecurityIT {

    @Autowired
    private MockMvc mockMvc;

    @Test
    void apiEndpoint_requiresAuth() throws Exception {
        mockMvc.perform(get("/api/content"))
                .andExpect(status().isUnauthorized());
    }

    @Test
    void apiEndpoint_allowsValidUser() throws Exception {
        mockMvc.perform(get("/api/content").with(httpBasic("admin", "changeme")))
                .andExpect(status().isOk());
    }

    @Test
    void apiEndpoint_rejectsBadCredentials() throws Exception {
        mockMvc.perform(get("/api/content").with(httpBasic("admin", "wrong")))
                .andExpect(status().isUnauthorized());
    }

    @Test
    void actuatorHealth_isPublic() throws Exception {
        mockMvc.perform(get("/actuator/health"))
                .andExpect(status().isOk());
    }

    @Test
    void actuatorMetrics_requiresActuatorRole() throws Exception {
        mockMvc.perform(get("/actuator/metrics"))
                .andExpect(status().isUnauthorized());
    }

    @Test
    void actuatorMetrics_allowsActuatorUser() throws Exception {
        mockMvc.perform(get("/actuator/metrics").with(httpBasic("actuator", "changeme")))
                .andExpect(status().isOk());
    }

    @Test
    void actuatorMetrics_rejectsAppUser() throws Exception {
        mockMvc.perform(get("/actuator/metrics").with(httpBasic("admin", "changeme")))
                .andExpect(status().isForbidden());
    }

    @Test
    void dashboard_requiresAuth() throws Exception {
        mockMvc.perform(get("/dashboard"))
                .andExpect(status().is3xxRedirection());
    }

    @Test
    void loginPage_isPublic() throws Exception {
        mockMvc.perform(get("/login"))
                .andExpect(status().isOk());
    }
}
