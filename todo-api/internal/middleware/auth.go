package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/flow-verify-round2/todo-api/internal/models"
)

type ctxKey string

const userCtxKey ctxKey = "user"

// UserFromContext returns the authenticated user id for the request, if any.
func UserFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userCtxKey).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// Auth requires a non-empty Bearer token and stores it as the user id.
// Paths listed in openPaths bypass authentication.
func Auth(openPaths map[string]struct{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, open := openPaths[r.URL.Path]; open {
				next.ServeHTTP(w, r)
				return
			}

			header := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(header, prefix) {
				writeAuthError(w, "missing or malformed Authorization header")
				return
			}
			token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
			if token == "" {
				writeAuthError(w, "bearer token must not be empty")
				return
			}

			ctx := context.WithValue(r.Context(), userCtxKey, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeAuthError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(models.APIError{
		Code:    "unauthenticated",
		Message: message,
	})
}
