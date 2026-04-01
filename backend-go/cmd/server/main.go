package main

import (
	"database/sql"
	"embed"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/gorilla/sessions"

	"github.com/oglimmer/easy-host/internal/auth"
	"github.com/oglimmer/easy-host/internal/config"
	"github.com/oglimmer/easy-host/internal/handler"
	"github.com/oglimmer/easy-host/internal/middleware"
	"github.com/oglimmer/easy-host/internal/service"
	"github.com/oglimmer/easy-host/internal/store"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func main() {
	cfg := config.Load()

	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := db.Ping(); err != nil {
		log.Fatalf("database ping failed: %v", err)
	}
	log.Println("connected to database")

	runMigrations(db)

	s := store.New(db)
	svc := service.NewContentService(s)
	users := auth.NewUserStore(cfg.AdminUsername, cfg.AdminPassword, cfg.ActuatorUsername, cfg.ActuatorPassword)
	sessionStore := sessions.NewCookieStore([]byte(cfg.SessionSecret))
	sessionStore.Options.HttpOnly = true
	sessionStore.Options.SameSite = http.SameSiteLaxMode
	sessionStore.Options.Secure = false // set to true behind HTTPS in prod

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:" + cfg.Port
	}

	var oidcHandler *handler.OIDCHandler
	if cfg.OIDCEnabled() {
		var err error
		oidcHandler, err = handler.NewOIDCHandler(cfg.OIDCIssuerURL, cfg.OIDCClientID, cfg.OIDCClientSecret, baseURL, cfg.OIDCAllowedUsers, sessionStore)
		if err != nil {
			log.Fatalf("OIDC init error: %v", err)
		}
		log.Printf("OIDC enabled (issuer: %s)", cfg.OIDCIssuerURL)
	}

	tmplDir := findTemplateDir()
	apiHandler := handler.NewAPIHandler(svc, cfg.MaxUploadSize)
	servingHandler := handler.NewServingHandler(svc)

	var oidcLogout handler.OIDCLogout
	if oidcHandler != nil {
		oidcLogout = oidcHandler
	}
	webHandler := handler.NewWebHandler(svc, users, sessionStore, tmplDir, cfg.MaxUploadSize, cfg.OIDCEnabled(), oidcLogout, cfg.OIDCClientID, baseURL)
	healthHandler := handler.NewHealthHandler(db)

	rateLimiter := middleware.NewRateLimiter()

	r := chi.NewRouter()
	r.Use(middleware.RequestLogger)
	r.Use(middleware.SecurityHeaders)
	r.Use(rateLimiter.Middleware)

	// Static files
	staticDir := findStaticDir()
	r.Handle("/css/*", http.StripPrefix("/css/", http.FileServer(http.Dir(filepath.Join(staticDir, "css")))))
	r.Handle("/js/*", http.StripPrefix("/js/", http.FileServer(http.Dir(filepath.Join(staticDir, "js")))))

	// Public routes
	r.Get("/login", webHandler.LoginPage)
	r.Post("/login", webHandler.LoginSubmit)
	if oidcHandler != nil {
		r.Get("/auth/login", oidcHandler.Login)
		r.Get("/auth/callback", oidcHandler.Callback)
	}
	r.Get("/developers", webHandler.DevelopersPage)
	r.Get("/imprint", webHandler.ImprintPage)
	r.Get("/privacy", webHandler.PrivacyPage)
	r.Get("/terms", webHandler.TermsPage)

	// Public serving
	r.Get("/s/{slug}", servingHandler.ServeIndex)
	r.Get("/s/{slug}/*", servingHandler.ServeFile)

	// Health endpoint (public, used by K8s probes)
	r.Get("/actuator/health", healthHandler.Health)

	// Other actuator endpoints (basic auth)
	r.Route("/actuator", func(r chi.Router) {
		r.Use(middleware.BasicAuth(users, "ACTUATOR"))
		r.Get("/info", healthHandler.Info)
	})

	// REST API (basic auth)
	r.Route("/api/content", func(r chi.Router) {
		r.Use(middleware.BasicAuth(users, "USER"))
		r.Get("/", apiHandler.ListContent)
		r.Post("/", apiHandler.CreateContent)
		r.Get("/{slug}", apiHandler.GetContent)
		r.Put("/{slug}", apiHandler.UpdateContent)
		r.Delete("/{slug}", apiHandler.DeleteContent)
	})

	// Web UI (session auth)
	r.Group(func(r chi.Router) {
		r.Use(middleware.SessionAuth(sessionStore))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/dashboard", http.StatusFound)
		})
		r.Get("/dashboard", webHandler.Dashboard)
		r.Get("/upload", webHandler.UploadPage)
		r.Post("/upload", webHandler.UploadSubmit)
		r.Get("/edit/{slug}", webHandler.EditPage)
		r.Post("/edit/{slug}", webHandler.EditSubmit)
		r.Post("/delete/{slug}", webHandler.DeleteSubmit)
		r.Post("/logout", webHandler.Logout)
	})

	log.Printf("starting server on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func runMigrations(db *sql.DB) {
	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		log.Fatalf("migration source error: %v", err)
	}
	driver, err := mysql.WithInstance(db, &mysql.Config{})
	if err != nil {
		log.Fatalf("migration driver error: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "mysql", driver)
	if err != nil {
		log.Fatalf("migration init error: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("migration error: %v", err)
	}
	log.Println("migrations applied")
}

func findTemplateDir() string {
	candidates := []string{
		"templates",
		"cmd/server/templates",
		filepath.Join(execDir(), "templates"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	log.Fatal("templates directory not found")
	return ""
}

func findStaticDir() string {
	candidates := []string{
		"static",
		"cmd/server/static",
		filepath.Join(execDir(), "static"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	log.Fatal("static directory not found")
	return ""
}

func execDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}
