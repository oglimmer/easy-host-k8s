package config

import (
	"os"
	"strings"
)

type Config struct {
	Port             string
	DSN              string
	ActuatorUsername  string
	ActuatorPassword  string
	AdminUsername     string
	AdminPassword     string
	SessionSecret    string
	MaxUploadSize    int64
	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCAllowedUsers []string
}

func (c *Config) OIDCEnabled() bool {
	return c.OIDCIssuerURL != ""
}

func Load() *Config {
	var allowedUsers []string
	if v := os.Getenv("OIDC_ALLOWED_USERS"); v != "" {
		for _, u := range strings.Split(v, ",") {
			if trimmed := strings.TrimSpace(u); trimmed != "" {
				allowedUsers = append(allowedUsers, trimmed)
			}
		}
	}
	return &Config{
		Port:             envOr("PORT", "8080"),
		DSN:              buildDSN(),
		ActuatorUsername:  envOr("ACTUATOR_USERNAME", "actuator"),
		ActuatorPassword:  envOr("ACTUATOR_PASSWORD", "changeme"),
		AdminUsername:     envOr("APP_ADMIN_USERNAME", "admin"),
		AdminPassword:     envOr("APP_ADMIN_PASSWORD", "changeme"),
		SessionSecret:    envOr("SESSION_SECRET", "change-me-in-production-32bytes!"),
		MaxUploadSize:    10 << 20, // 10MB
		OIDCIssuerURL:    os.Getenv("OIDC_ISSUER_URL"),
		OIDCClientID:     os.Getenv("OIDC_CLIENT_ID"),
		OIDCClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		OIDCAllowedUsers: allowedUsers,
	}
}

func buildDSN() string {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}
	// Parse SPRING_DATASOURCE_URL (jdbc:mariadb://host:port/db) if set
	if jdbcURL := os.Getenv("SPRING_DATASOURCE_URL"); jdbcURL != "" {
		return parseJDBCURL(jdbcURL)
	}
	host := envOr("DB_HOST", "localhost")
	port := envOr("DB_PORT", "3306")
	user := envOr("SPRING_DATASOURCE_USERNAME", "easyhost")
	pass := envOr("SPRING_DATASOURCE_PASSWORD", "easyhost")
	name := envOr("DB_NAME", "easyhost")
	return user + ":" + pass + "@tcp(" + host + ":" + port + ")/" + name + "?parseTime=true&charset=utf8mb4&multiStatements=true"
}

// parseJDBCURL converts jdbc:mariadb://host:port/db into Go DSN format.
func parseJDBCURL(jdbc string) string {
	// Strip jdbc:mariadb:// or jdbc:mysql:// prefix
	s := jdbc
	for _, prefix := range []string{"jdbc:mariadb://", "jdbc:mysql://"} {
		if strings.HasPrefix(s, prefix) {
			s = s[len(prefix):]
			break
		}
	}
	// s is now "host:port/db" or "host:port/db?params"
	user := envOr("SPRING_DATASOURCE_USERNAME", "easyhost")
	pass := envOr("SPRING_DATASOURCE_PASSWORD", "easyhost")
	// Separate host:port/db from any JDBC params (we use our own)
	if idx := strings.Index(s, "?"); idx != -1 {
		s = s[:idx]
	}
	// Split into host:port and dbname
	parts := strings.SplitN(s, "/", 2)
	hostPort := parts[0]
	dbName := "easyhost"
	if len(parts) == 2 && parts[1] != "" {
		dbName = parts[1]
	}
	return user + ":" + pass + "@tcp(" + hostPort + ")/" + dbName + "?parseTime=true&charset=utf8mb4&multiStatements=true"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
