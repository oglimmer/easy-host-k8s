package com.oglimmer.easyhost.config;

import java.util.Arrays;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

import org.springframework.beans.factory.ObjectProvider;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.autoconfigure.condition.ConditionalOnExpression;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.core.annotation.Order;
import org.springframework.security.config.Customizer;
import org.springframework.security.config.annotation.web.builders.HttpSecurity;
import org.springframework.security.config.annotation.web.configuration.EnableWebSecurity;
import org.springframework.security.core.GrantedAuthority;
import org.springframework.security.core.authority.SimpleGrantedAuthority;
import org.springframework.security.core.userdetails.User;
import org.springframework.security.core.userdetails.UserDetailsService;
import org.springframework.security.crypto.bcrypt.BCryptPasswordEncoder;
import org.springframework.security.crypto.password.PasswordEncoder;
import org.springframework.security.oauth2.client.oidc.userinfo.OidcUserRequest;
import org.springframework.security.oauth2.client.oidc.userinfo.OidcUserService;
import org.springframework.security.oauth2.client.oidc.web.logout.OidcClientInitiatedLogoutSuccessHandler;
import org.springframework.security.oauth2.client.registration.ClientRegistration;
import org.springframework.security.oauth2.client.registration.ClientRegistrationRepository;
import org.springframework.security.oauth2.client.registration.ClientRegistrations;
import org.springframework.security.oauth2.client.registration.InMemoryClientRegistrationRepository;
import org.springframework.security.oauth2.client.userinfo.OAuth2UserService;
import org.springframework.security.oauth2.core.AuthorizationGrantType;
import org.springframework.security.oauth2.core.OAuth2AuthenticationException;
import org.springframework.security.oauth2.core.oidc.OidcIdToken;
import org.springframework.security.oauth2.core.oidc.user.DefaultOidcUser;
import org.springframework.security.oauth2.core.oidc.user.OidcUser;
import org.springframework.security.provisioning.InMemoryUserDetailsManager;
import org.springframework.security.web.SecurityFilterChain;
import org.springframework.security.web.authentication.logout.LogoutSuccessHandler;
import lombok.extern.slf4j.Slf4j;

@Configuration
@EnableWebSecurity
@Slf4j
public class SecurityConfig {

    @Value("${actuator.username}")
    private String actuatorUsername;

    @Value("${actuator.password}")
    private String actuatorPassword;

    @Value("${app.admin.username}")
    private String adminUsername;

    @Value("${app.admin.password}")
    private String adminPassword;

    @Value("${app.oidc.allowed-users:}")
    private String allowedUsers;

    @Bean
    @Order(1)
    public SecurityFilterChain apiFilterChain(HttpSecurity http) throws Exception {
        http
            .securityMatcher("/api/**", "/actuator/**")
            .authorizeHttpRequests(auth -> auth
                .requestMatchers("/actuator/health").permitAll()
                .requestMatchers("/actuator/**").hasRole("ACTUATOR")
                .requestMatchers("/api/**").hasRole("USER")
            )
            .httpBasic(Customizer.withDefaults())
            .csrf(csrf -> csrf.disable())
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

    @Bean
    @Order(2)
    public SecurityFilterChain servingFilterChain(HttpSecurity http) throws Exception {
        http
            .securityMatcher("/s/**")
            .authorizeHttpRequests(auth -> auth.anyRequest().permitAll())
            .headers(headers -> headers
                .contentSecurityPolicy(csp -> csp
                    .policyDirectives("default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; frame-ancestors 'none'")
                )
                .contentTypeOptions(Customizer.withDefaults())
                .frameOptions(fo -> fo.deny())
            );
        return http.build();
    }

    @Bean
    @Order(3)
    public SecurityFilterChain webFilterChain(HttpSecurity http,
                                              ObjectProvider<ClientRegistrationRepository> clientRegProvider) throws Exception {
        http
            .authorizeHttpRequests(auth -> auth
                .requestMatchers("/login").permitAll()
                .requestMatchers("/dashboard/**", "/upload", "/edit/**", "/delete/**").hasRole("USER")
                .anyRequest().permitAll()
            )
            .formLogin(form -> form
                .loginPage("/login")
                .defaultSuccessUrl("/dashboard", true)
            )
            .headers(headers -> headers
                .contentTypeOptions(Customizer.withDefaults())
                .frameOptions(fo -> fo.deny())
                .httpStrictTransportSecurity(hsts -> hsts
                    .includeSubDomains(true)
                    .maxAgeInSeconds(31536000)
                )
            );

        ClientRegistrationRepository clientRegistrationRepository = clientRegProvider.getIfAvailable();
        if (clientRegistrationRepository != null) {
            http
                .oauth2Login(oauth2 -> oauth2
                    .loginPage("/login")
                    .userInfoEndpoint(userInfo -> userInfo.oidcUserService(oidcUserService()))
                    .defaultSuccessUrl("/dashboard", true)
                )
                .logout(logout -> logout
                    .logoutSuccessHandler(oidcLogoutSuccessHandler(clientRegistrationRepository))
                );
        } else {
            http.logout(logout -> logout
                .logoutSuccessUrl("/login?logout")
            );
        }

        return http.build();
    }

    @Bean
    @ConditionalOnExpression("!'${app.oidc.issuer-url:}'.isEmpty()")
    public ClientRegistrationRepository clientRegistrationRepository(
            @Value("${app.oidc.issuer-url}") String issuerUrl,
            @Value("${app.oidc.client-id}") String clientId,
            @Value("${app.oidc.client-secret}") String clientSecret) {
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
    public UserDetailsService userDetailsService(PasswordEncoder encoder) {
        var actuator = User.builder()
            .username(actuatorUsername)
            .password(encoder.encode(actuatorPassword))
            .roles("ACTUATOR")
            .build();
        var admin = User.builder()
            .username(adminUsername)
            .password(encoder.encode(adminPassword))
            .roles("USER")
            .build();
        return new InMemoryUserDetailsManager(actuator, admin);
    }

    @Bean
    public PasswordEncoder passwordEncoder() {
        return new BCryptPasswordEncoder();
    }

    private LogoutSuccessHandler oidcLogoutSuccessHandler(ClientRegistrationRepository clientRegistrationRepository) {
        OidcClientInitiatedLogoutSuccessHandler handler =
                new OidcClientInitiatedLogoutSuccessHandler(clientRegistrationRepository);
        handler.setPostLogoutRedirectUri("{baseUrl}/");
        return handler;
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

            OidcIdToken idToken = oidcUser.getIdToken();
            String issuer = idToken.getIssuer().toString();
            String subject = idToken.getSubject();
            String principalName = issuer + "|" + subject;

            Set<GrantedAuthority> authorities = new HashSet<>(oidcUser.getAuthorities());
            authorities.add(new SimpleGrantedAuthority("ROLE_USER"));

            return new DefaultOidcUser(authorities, idToken, oidcUser.getUserInfo()) {
                @Override
                public String getName() {
                    return principalName;
                }
            };
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
