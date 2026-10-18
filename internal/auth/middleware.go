package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"gitsync/internal/respond"
)

type userContextKey struct{}

// UserFromContext returns the identity populated by RequireSession.
func ContextWithUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, userContextKey{}, user)
}

func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userContextKey{}).(User)
	return user, ok
}

// RequireSession rejects requests without a valid session; it never redirects.
func (s *Service) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.SessionUser(r)
		if err != nil {
			respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	})
}

// Me returns the authenticated identity in the API contract shape.
func Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	respond.JSON(w, http.StatusOK, user)
}

// NewCSRFProtection constructs the application-wide same-origin protection.
// Hook routes are exempt because they authenticate their requests independently.
func NewCSRFProtection(baseURL string) (*http.CrossOriginProtection, error) {
	protection := http.NewCrossOriginProtection()
	protection.AddInsecureBypassPattern("/hooks/")
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL for CSRF protection: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("base URL for CSRF protection must have a scheme and host")
	}
	origin := parsed.Scheme + "://" + parsed.Host
	if err := protection.AddTrustedOrigin(origin); err != nil {
		return nil, err
	}
	return protection, nil
}
