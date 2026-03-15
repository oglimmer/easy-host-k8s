package com.oglimmer.easyhost.filter;

import jakarta.servlet.FilterChain;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;
import org.springframework.mock.web.MockHttpServletRequest;
import org.springframework.mock.web.MockHttpServletResponse;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.Mockito.*;

class RateLimitFilterTest {

    private final RateLimitFilter filter = new RateLimitFilter();

    @Test
    void allowsRequestsWithinLimit() throws Exception {
        MockHttpServletRequest request = new MockHttpServletRequest();
        request.setRemoteAddr("10.0.0.1");
        MockHttpServletResponse response = new MockHttpServletResponse();
        FilterChain chain = mock(FilterChain.class);

        filter.doFilterInternal(request, response, chain);

        verify(chain).doFilter(request, response);
        assertThat(response.getStatus()).isEqualTo(HttpStatus.OK.value());
    }

    @Test
    void usesXForwardedForHeader() throws Exception {
        MockHttpServletRequest request = new MockHttpServletRequest();
        request.setRemoteAddr("10.0.0.1");
        request.addHeader("X-Forwarded-For", "192.168.1.100, 10.0.0.1");
        MockHttpServletResponse response = new MockHttpServletResponse();
        FilterChain chain = mock(FilterChain.class);

        // First request from this XFF IP should pass
        filter.doFilterInternal(request, response, chain);

        verify(chain).doFilter(request, response);
    }

    @Test
    void blocksExcessiveRequests() throws Exception {
        FilterChain chain = mock(FilterChain.class);
        String testIp = "10.99.99.99";

        // Exhaust the rate limiter — Guava RateLimiter allows 10/sec
        // The first tryAcquire succeeds (uses the stored permit), subsequent ones may fail
        int blocked = 0;
        for (int i = 0; i < 20; i++) {
            MockHttpServletRequest request = new MockHttpServletRequest();
            request.setRemoteAddr(testIp);
            MockHttpServletResponse response = new MockHttpServletResponse();
            filter.doFilterInternal(request, response, chain);
            if (response.getStatus() == HttpStatus.TOO_MANY_REQUESTS.value()) {
                blocked++;
            }
        }

        assertThat(blocked).isGreaterThan(0);
    }
}
