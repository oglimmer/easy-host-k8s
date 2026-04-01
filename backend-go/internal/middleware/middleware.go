package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/sessions"
	"github.com/oglimmer/easy-host/internal/auth"
	"golang.org/x/time/rate"
)

type contextKey string

const UserKey contextKey = "user"

func GetUser(r *http.Request) *auth.User {
	u, _ := r.Context().Value(UserKey).(*auth.User)
	return u
}

// BasicAuth middleware for API endpoints.
func BasicAuth(users *auth.UserStore, requiredRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username, password, ok := r.BasicAuth()
			if !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="easy-host"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			user := users.Authenticate(username, password)
			if user == nil || user.Role != requiredRole {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			ctx := context.WithValue(r.Context(), UserKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SessionAuth middleware for web endpoints using cookie sessions.
func SessionAuth(sessionStore *sessions.CookieStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, _ := sessionStore.Get(r, "session")
			username, ok := session.Values["username"].(string)
			if !ok || username == "" {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}
			user := &auth.User{Username: username, Role: "USER"}
			ctx := context.WithValue(r.Context(), UserKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SecurityHeaders adds common security headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// RateLimiter provides per-IP rate limiting.
type RateLimiter struct {
	visitors map[string]*visitorEntry
	rate     rate.Limit
	burst    int
}

type visitorEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitorEntry),
		rate:     10,
		burst:    10,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) getVisitor(ip string) *rate.Limiter {
	v, exists := rl.visitors[ip]
	if !exists {
		limiter := rate.NewLimiter(rl.rate, rl.burst)
		rl.visitors[ip] = &visitorEntry{limiter: limiter, lastSeen: time.Now()}
		return limiter
	}
	v.lastSeen = time.Now()
	return v.limiter
}

func (rl *RateLimiter) cleanup() {
	for {
		time.Sleep(time.Minute)
		for ip, v := range rl.visitors {
			if time.Since(v.lastSeen) > time.Hour {
				delete(rl.visitors, ip)
			}
		}
	}
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	excluded := []string{"/login", "/dashboard", "/upload", "/edit/", "/delete/", "/actuator", "/css/", "/js/", "/fonts/", "/"}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		for _, ex := range excluded {
			if path == ex || (ex != "/" && strings.HasPrefix(path, ex)) {
				next.ServeHTTP(w, r)
				return
			}
		}
		ip := clientIP(r)
		if !rl.getVisitor(ip).Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	return r.RemoteAddr
}

// RequestLogger logs HTTP requests.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &responseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(ww, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, ww.status, time.Since(start))
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}
