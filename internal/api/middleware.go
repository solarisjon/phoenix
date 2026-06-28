package api

import (
	"context"
	"net/http"
	"time"

	"github.com/solarisjon/phoenix/internal/config"
	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/store"
)

type contextKey int

const userContextKey contextKey = 1

// userFromCtx retrieves the authenticated user injected by authMiddleware.
func userFromCtx(ctx context.Context) *model.User {
	u, _ := ctx.Value(userContextKey).(*model.User)
	return u
}

// authMiddleware injects the current user into the request context.
//
// When auth is disabled (single-user mode) it loads the default user and
// passes the request through — identical to the pre-auth behaviour.
// When auth is enabled it validates the session cookie and returns 401 on
// missing or expired sessions.
func authMiddleware(cfg config.Config, users store.UserRepo, sessions store.SessionRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.AuthEnabled {
				u, err := users.GetDefault(r.Context())
				if err != nil || u == nil {
					respondErr(w, http.StatusInternalServerError, "failed to load default user")
					return
				}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey, u)))
				return
			}

			cookie, err := r.Cookie("phoenix_session")
			if err != nil {
				respondErr(w, http.StatusUnauthorized, "authentication required")
				return
			}

			sess, err := sessions.GetByID(r.Context(), cookie.Value)
			if err != nil {
				respondErr(w, http.StatusInternalServerError, "session lookup failed")
				return
			}
			if sess == nil || sess.ExpiresAt.Before(time.Now()) {
				respondErr(w, http.StatusUnauthorized, "session expired")
				return
			}

			u, err := users.Get(r.Context(), sess.UserID)
			if err != nil || u == nil {
				respondErr(w, http.StatusUnauthorized, "user not found")
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey, u)))
		})
	}
}
