package com.oglimmer.easyhost;

import org.junit.jupiter.api.Test;

class EasyHostApplicationTest {

    @Test
    void contextLoadsWithoutSpring() {
        // Smoke test: main class exists
        EasyHostApplication.main(new String[]{"--spring.main.web-application-type=none", "--spring.autoconfigure.exclude=org.springframework.boot.autoconfigure.jdbc.DataSourceAutoConfiguration,org.springframework.boot.autoconfigure.orm.jpa.HibernateJpaAutoConfiguration,org.springframework.boot.autoconfigure.flyway.FlywayAutoConfiguration"});
    }
}
