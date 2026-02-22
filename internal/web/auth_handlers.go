package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/perbu/science-newsletter/internal/auth"
	"github.com/perbu/science-newsletter/internal/database/db"
)

func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	h.renderPage(w, r, "login.html.tmpl", nil)
}

func (h *Handler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		h.renderPage(w, r, "login.html.tmpl", map[string]any{"Error": "Email is required."})
		return
	}

	// Always show "check your email" to prevent email enumeration
	if auth.IsAllowed(email, h.cfg.Auth.AllowedEmails) {
		token, err := auth.GenerateToken()
		if err != nil {
			slog.Error("generate auth token", "err", err)
			h.renderPage(w, r, "login.html.tmpl", map[string]any{"Error": "Something went wrong. Please try again."})
			return
		}

		expiresAt := time.Now().Add(time.Duration(h.cfg.Auth.TokenExpiry) * time.Minute)
		if err := h.queries.CreateAuthToken(r.Context(), db.CreateAuthTokenParams{
			Token:     token,
			Email:     email,
			ExpiresAt: expiresAt,
		}); err != nil {
			slog.Error("store auth token", "err", err)
			h.renderPage(w, r, "login.html.tmpl", map[string]any{"Error": "Something went wrong. Please try again."})
			return
		}

		link := strings.TrimRight(h.cfg.Auth.BaseURL, "/") + "/auth/verify?token=" + token

		if h.mailer != nil {
			body := magicLinkEmailHTML(link, h.cfg.Auth.TokenExpiry)
			if err := h.mailer.Send(r.Context(), []string{email}, "Sign in to Science Newsletter", body); err != nil {
				slog.Error("send magic link email", "err", err, "email", email)
			} else {
				slog.Info("magic link sent", "email", email)
			}
		} else {
			slog.Info("magic link (no mailer configured)", "email", email, "url", link)
		}
	} else {
		slog.Debug("login attempt from non-allowed email", "email", email)
	}

	h.renderPage(w, r, "check_email.html.tmpl", map[string]any{
		"TokenExpiry": h.cfg.Auth.TokenExpiry,
	})
}

func (h *Handler) VerifyTokenPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		h.renderPage(w, r, "login.html.tmpl", map[string]any{"Error": "Invalid or expired link."})
		return
	}

	// Check that the token exists and is valid, but do NOT consume it.
	// Email security scanners follow GET links; they'll reach this page
	// but won't submit the form, so the token stays available for the user.
	_, err := h.queries.GetAuthTokenByToken(r.Context(), token)
	if err != nil {
		slog.Debug("invalid auth token", "err", err)
		h.renderPage(w, r, "login.html.tmpl", map[string]any{"Error": "Invalid or expired link."})
		return
	}

	h.renderPage(w, r, "verify_confirm.html.tmpl", map[string]any{"Token": token})
}

func (h *Handler) VerifyTokenSubmit(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")
	if token == "" {
		h.renderPage(w, r, "login.html.tmpl", map[string]any{"Error": "Invalid or expired link."})
		return
	}

	authToken, err := h.queries.GetAuthTokenByToken(r.Context(), token)
	if err != nil {
		slog.Debug("invalid auth token", "err", err)
		h.renderPage(w, r, "login.html.tmpl", map[string]any{"Error": "Invalid or expired link."})
		return
	}

	if !auth.IsAllowed(authToken.Email, h.cfg.Auth.AllowedEmails) {
		slog.Warn("sign-in attempt from non-allowed email", "email", authToken.Email)
		h.renderPage(w, r, "login.html.tmpl", map[string]any{"Error": "Your email address is not authorized. Please contact the administrator."})
		return
	}

	if err := h.queries.MarkAuthTokenUsed(r.Context(), token); err != nil {
		slog.Error("mark auth token used", "err", err)
	}

	sessionToken, err := auth.GenerateToken()
	if err != nil {
		slog.Error("generate session token", "err", err)
		h.renderPage(w, r, "login.html.tmpl", map[string]any{"Error": "Something went wrong. Please try again."})
		return
	}

	expiresAt := time.Now().Add(time.Duration(h.cfg.Auth.SessionMaxAge) * time.Hour)
	if err := h.queries.CreateSession(r.Context(), db.CreateSessionParams{
		Token:     sessionToken,
		Email:     authToken.Email,
		ExpiresAt: expiresAt,
	}); err != nil {
		slog.Error("create session", "err", err)
		h.renderPage(w, r, "login.html.tmpl", map[string]any{"Error": "Something went wrong. Please try again."})
		return
	}

	auth.SetSessionCookie(w, sessionToken, h.cfg.Auth)
	slog.Info("user signed in", "email", authToken.Email)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil && cookie.Value != "" {
		if err := h.queries.DeleteSession(r.Context(), cookie.Value); err != nil {
			slog.Error("delete session", "err", err)
		}
	}
	auth.ClearSessionCookie(w, h.cfg.Auth)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func magicLinkEmailHTML(link string, expiryMinutes int) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family: sans-serif; max-width: 480px; margin: 0 auto; padding: 20px;">
    <h2 style="color: #4338ca;">Science Newsletter</h2>
    <p>Click the button below to sign in. This link expires in %d minutes.</p>
    <p style="text-align: center; margin: 30px 0;">
        <a href="%s"
           style="background-color: #4338ca; color: white; padding: 12px 24px; text-decoration: none; border-radius: 6px; display: inline-block;">
            Sign in
        </a>
    </p>
    <p style="color: #6b7280; font-size: 14px;">If you didn't request this, you can ignore this email.</p>
</body>
</html>`, expiryMinutes, link)
}
