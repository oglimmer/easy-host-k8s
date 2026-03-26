package com.oglimmer.easyhost.config;

import java.util.Arrays;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.core.annotation.Order;
import org.springframework.security.config.Customizer;
import org.springframework.security.config.annotation.web.builders.HttpSecurity;
import org.springframework.security.core.GrantedAuthority;
import org.springframework.security.core.authority.SimpleGrantedAuthority;
import org.springframework.security.oauth2.client.oidc.userinfo.OidcUserRequest;
import org.springframework.security.oauth2.client.oidc.userinfo.OidcUserService;
import org.springframework.security.oauth2.client.registration.ClientRegistration;
import org.springframework.security.oauth2.client.registration.ClientRegistrationRepository;
import org.springframework.security.oauth2.client.registration.ClientRegistrations;
import org.springframework.security.oauth2.client.registration.InMemoryClientRegistrationRepository;
import org.springframework.security.oauth2.client.userinfo.OAuth2UserService;
import org.springframework.security.oauth2.core.AuthorizationGrantType;
import org.springframework.security.oauth2.core.OAuth2AuthenticationException;
import org.springframework.security.oauth2.core.oidc.user.DefaultOidcUser;
import org.springframework.security.oauth2.core.oidc.user.OidcUser;
import org.springframework.security.web.SecurityFilterChain;
import lombok.extern.slf4j.Slf4j;

@Configuration
@ConditionalOnProperty(name = "app.auth.mode", havingValue = "oidc")
@Slf4j
public class OidcSecurityConfig {

    @Value("${app.oidc.issuer-url}")
    private String issuerUrl;

    @Value("${app.oidc.client-id}")
    private String clientId;

    @Value("${app.oidc.client-secret}")
    private String clientSecret;

    @Value("${app.oidc.allowed-users:}")
    private String allowedUsers;

    @Bean
    public ClientRegistrationRepository clientRegistrationRepository() {
        ClientRegistration registration = ClientRegistrations.fromIssuerLocation(issuerUrl)
                .registrationId("keycloak")
                .clientId(clientId)
                .clientSecret(clientSecret)
                .scope("openid", "email")
                .authorizationGrantType(AuthorizationGrantType.AUTHORIZATION_CODE)
                .redirectUri("{baseUrl}/login/oauth2/code/{registrationId}")
                .userNameAttributeName("email")
                .build();
        return new InMemoryClientRegistrationRepository(registration);
    }

    @Bean
    @Order(3)
    public SecurityFilterChain oidcWebFilterChain(HttpSecurity http) throws Exception {
        http
            .authorizeHttpRequests(auth -> auth
                .requestMatchers("/dashboard/**", "/upload", "/edit/**", "/delete/**").hasRole("USER")
                .anyRequest().permitAll()
            )
            .oauth2Login(oauth2 -> oauth2
                .userInfoEndpoint(userInfo -> userInfo.oidcUserService(oidcUserService()))
                .defaultSuccessUrl("/dashboard", true)
            )
            .logout(logout -> logout
                .logoutSuccessUrl("/")
            )
            .headers(headers -> headers
                .contentTypeOptions(Customizer.withDefaults())
                .frameOptions(fo -> fo.deny())
                .httpStrictTransportSecurity(hsts -> hsts
                    .includeSubDomains(true)
                    .maxAgeInSeconds(31536000)
                )
            );
        return http.build();
    }

    private OAuth2UserService<OidcUserRequest, OidcUser> oidcUserService() {
        OidcUserService delegate = new OidcUserService();
        return request -> {
            OidcUser oidcUser = delegate.loadUser(request);
            String email = oidcUser.getEmail();

            List<String> allowed = parseAllowedUsers();
            if (!allowed.isEmpty() && !allowed.contains(email)) {
                log.warn("OIDC login denied for user: {}", email);
                throw new OAuth2AuthenticationException("User not allowed: " + email);
            }

            Set<GrantedAuthority> authorities = new HashSet<>(oidcUser.getAuthorities());
            authorities.add(new SimpleGrantedAuthority("ROLE_USER"));

            return new DefaultOidcUser(authorities, oidcUser.getIdToken(), oidcUser.getUserInfo(), "email");
        };
    }

    private List<String> parseAllowedUsers() {
        if (allowedUsers == null || allowedUsers.isBlank()) {
            return List.of();
        }
        return Arrays.stream(allowedUsers.split(","))
                .map(String::trim)
                .filter(s -> !s.isEmpty())
                .toList();
    }
}
