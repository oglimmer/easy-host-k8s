# easy-host

Simple web content hosting service. Upload HTML/files via API, serve them on unique URLs.

## Tech Stack

- **Backend:** Spring Boot 3.4 (Java 21), Spring Security, Spring Data JPA, Thymeleaf
- **Database:** MariaDB 11 with Flyway migrations
- **Metrics:** Micrometer + Prometheus
- **Deployment:** Docker, Helm, Kubernetes
- **CI/CD:** GitHub Actions

## Local Development

Start the database and backend with Docker Compose:

```bash
docker compose up
```

The backend is available at `http://localhost:8080`.

### Run backend locally (against dockerized DB)

```bash
docker compose up db
cd backend
./mvnw spring-boot:run
```

### Run tests

```bash
cd backend
./mvnw test                # unit tests
./mvnw verify              # unit + integration tests (requires Docker for Testcontainers)
```

## Configuration

Environment variables:

| Variable | Description | Default |
|---|---|---|
| `SPRING_DATASOURCE_URL` | JDBC connection URL | `jdbc:mariadb://localhost:3306/easyhost` |
| `SPRING_DATASOURCE_USERNAME` | DB username | `easyhost` |
| `SPRING_DATASOURCE_PASSWORD` | DB password | `easyhost` |
| `ACTUATOR_USERNAME` | Actuator endpoint username | — |
| `ACTUATOR_PASSWORD` | Actuator endpoint password | — |
| `APP_USER_USERNAME` | Application admin username | — |
| `APP_USER_PASSWORD` | Application admin password | — |

## Deployment

A Helm chart is provided in `helm/easy-host/`. The CI pipeline builds and pushes a Docker image on every push to `main`.
