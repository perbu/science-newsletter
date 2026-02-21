package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/perbu/science-newsletter/internal/config"
	"github.com/perbu/science-newsletter/internal/database/db"
)

type contextKey string

const emailContextKey contextKey = "auth_email"

const sessionCookieName = "session"

// GenerateToken returns a cryptographically random 32-byte hex-encoded token.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// EmailFromContext returns the authenticated email from the request context, or "".
func EmailFromContext(ctx context.Context) string {
	v, _ := ctx.Value(emailContextKey).(string)
	return v
}

// Middleware checks the session cookie and redirects unauthenticated requests to /login.
// Public paths (/login, /auth/verify) are exempt.
func Middleware(queries *db.Queries, cfg config.AuthConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow public paths through without auth
		cleanPath := path.Clean(r.URL.Path)
		if cleanPath == "/login" || cleanPath == "/auth/verify" {
			next.ServeHTTP(w, r)
			return
		}

		// If no allowed emails configured, skip auth entirely (open access)
		if len(cfg.AllowedEmails) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		session, err := queries.GetSessionByToken(r.Context(), cookie.Value)
		if err != nil {
			slog.Debug("invalid session cookie", "err", err)
			ClearSessionCookie(w, cfg)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		ctx := context.WithValue(r.Context(), emailContextKey, session.Email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SetSessionCookie sets the session cookie on the response.
func SetSessionCookie(w http.ResponseWriter, token string, cfg config.AuthConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   cfg.SessionMaxAge * 3600,
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie removes the session cookie.
func ClearSessionCookie(w http.ResponseWriter, cfg config.AuthConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// IsAllowed checks if the email is in the allowed list (case-insensitive).
func IsAllowed(email string, allowedEmails []string) bool {
	lower := strings.ToLower(email)
	for _, allowed := range allowedEmails {
		if strings.ToLower(allowed) == lower {
			return true
		}
	}
	return false
}

// CleanupExpired removes expired auth tokens and sessions.
func CleanupExpired(ctx context.Context, queries *db.Queries) {
	if err := queries.DeleteExpiredAuthTokens(ctx); err != nil {
		slog.Error("cleanup expired auth tokens", "err", err)
	}
	if err := queries.DeleteExpiredSessions(ctx); err != nil {
		slog.Error("cleanup expired sessions", "err", err)
	}
}

// StartCleanup runs periodic cleanup of expired tokens and sessions.
func StartCleanup(ctx context.Context, queries *db.Queries, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				CleanupExpired(ctx, queries)
			}
		}
	}()
}
