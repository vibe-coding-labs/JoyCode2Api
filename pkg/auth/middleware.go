package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

type contextKey string

const usernameKey contextKey = "auth_username"

type SettingsGetter interface {
	GetSetting(key string) string
}

func JWTMiddleware(getter SettingsGetter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		whitelist := map[string]bool{
			"/api/auth/status":     true,
			"/api/auth/setup":      true,
			"/api/auth/login":      true,
			"/api/health":          true,
			"/api/github-stars":    true,
			"/api/browser-login":   true,
			"/api/oauth-callback":  true,
			"/api/oauth-submit":    true,
		}
		if whitelist[path] {
			next.ServeHTTP(w, r)
			return
		}

		hash := getter.GetSetting("auth_password_hash")
		if hash == "" {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeAuthError(w, "missing authorization header")
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenStr == authHeader {
			writeAuthError(w, "invalid authorization format, expected Bearer token")
			return
		}

		secret := getter.GetSetting("auth_jwt_secret")
		if secret == "" {
			slog.Error("auth: JWT secret not configured")
			writeAuthError(w, "server configuration error")
			return
		}

		claims, err := ValidateToken(tokenStr, secret)
		if err != nil {
			slog.Warn("auth: JWT validation failed", "error", err, "path", path)
			writeAuthError(w, "invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), usernameKey, claims.Username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusUnauthorized)
	// Use json.Marshal so msg is properly escaped (avoids JSON injection if
	// msg ever contains quotes/backslashes).
	if data, err := json.Marshal(map[string]string{"detail": msg}); err == nil {
		w.Write(data)
	}
}

func AuthenticatedUser(r *http.Request) string {
	if v, ok := r.Context().Value(usernameKey).(string); ok {
		return v
	}
	return ""
}
