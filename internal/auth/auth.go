// Package auth implements OIDC login and encrypted cookie sessions.
package auth

import (
	"context"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/securecookie"
	"golang.org/x/oauth2"

	"gitsync/internal/config"
)

const (
	TransientCookieName = "gitsync_oidc"
	SessionCookieName   = "gitsync_session"
	transientTTL        = 10 * time.Minute
)

var errInvalidSession = errors.New("invalid session")

// User is the authenticated OIDC identity. All authenticated users have the admin role.
type User struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

type flowState struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Next     string `json:"next"`
}

// Service owns the discovered OIDC provider and cookie codecs.
type Service struct {
	oauth     oauth2.Config
	verifier  *oidc.IDTokenVerifier
	transient *securecookie.SecureCookie
	session   *securecookie.SecureCookie
	ttl       time.Duration
	secure    bool
}

// New discovers the provider, retrying five times with a two second delay. An HTTP client
// may be supplied in ctx with oidc.ClientContext, which is useful for custom trust roots.
func New(ctx context.Context, cfg config.Config) (*Service, error) {
	var provider *oidc.Provider
	var err error
	for attempt := range 5 {
		discoveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		provider, err = oidc.NewProvider(discoveryCtx, cfg.OIDC.IssuerURL)
		cancel()
		if err == nil {
			break
		}
		if attempt == 4 {
			return nil, fmt.Errorf("auth: discover mandatory OIDC provider %q after 5 attempts: %w", cfg.OIDC.IssuerURL, err)
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("auth: discover OIDC provider: %w", ctx.Err())
		case <-timer.C:
		}
	}

	hashKey, blockKey, err := DeriveCookieKeys(cfg.OIDC.SessionSecret)
	if err != nil {
		return nil, fmt.Errorf("auth: derive cookie keys: %w", err)
	}
	ttl := time.Duration(cfg.OIDC.SessionTTL)
	transientCookie := securecookie.New(hashKey, blockKey).MaxAge(int(transientTTL.Seconds()))
	sessionCookie := securecookie.New(hashKey, blockKey).MaxAge(int(ttl.Seconds()))
	redirectURL := cfg.OIDC.RedirectURL
	if redirectURL == "" {
		redirectURL = strings.TrimRight(cfg.Server.BaseURL, "/") + "/auth/callback"
	}
	scopes := append([]string(nil), cfg.OIDC.Scopes...)
	if !contains(scopes, "openid") {
		scopes = append([]string{"openid"}, scopes...)
	}
	return &Service{
		oauth: oauth2.Config{
			ClientID: cfg.OIDC.ClientID, ClientSecret: cfg.OIDC.ClientSecret,
			Endpoint: provider.Endpoint(), RedirectURL: redirectURL, Scopes: scopes,
		},
		verifier:  provider.Verifier(&oidc.Config{ClientID: cfg.OIDC.ClientID}),
		transient: transientCookie, session: sessionCookie, ttl: ttl,
		secure: strings.EqualFold(urlScheme(cfg.Server.BaseURL), "https"),
	}, nil
}

// DeriveCookieKeys derives independent securecookie signing and encryption keys using HKDF-SHA256.
func DeriveCookieKeys(secret string) ([]byte, []byte, error) {
	if len(secret) < 32 {
		return nil, nil, errors.New("session secret must be at least 32 bytes")
	}
	hashKey, err := hkdf.Key(sha256.New, []byte(secret), nil, "gitsync/session/hash/v1", 32)
	if err != nil {
		return nil, nil, fmt.Errorf("derive signing key: %w", err)
	}
	blockKey, err := hkdf.Key(sha256.New, []byte(secret), nil, "gitsync/session/block/v1", 32)
	if err != nil {
		return nil, nil, fmt.Errorf("derive encryption key: %w", err)
	}
	return hashKey, blockKey, nil
}

// Login begins the authorization-code flow with state, nonce, and PKCE.
func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken(32)
	if err != nil {
		http.Error(w, "could not start login", http.StatusInternalServerError)
		return
	}
	nonce, err := randomToken(32)
	if err != nil {
		http.Error(w, "could not start login", http.StatusInternalServerError)
		return
	}
	verifier, err := randomToken(32)
	if err != nil {
		http.Error(w, "could not start login", http.StatusInternalServerError)
		return
	}
	flow := flowState{State: state, Nonce: nonce, Verifier: verifier, Next: SanitizeNext(r.URL.Query().Get("next"))}
	encoded, err := s.transient.Encode(TransientCookieName, flow)
	if err != nil {
		http.Error(w, "could not start login", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: TransientCookieName, Value: encoded, Path: "/auth/", MaxAge: int(transientTTL.Seconds()),
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode,
	})
	authorizeURL := s.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, authorizeURL, http.StatusFound)
}

// Callback verifies the authorization response and creates a session cookie.
func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(TransientCookieName)
	if err != nil {
		s.clearTransient(w)
		http.Error(w, "invalid login session", http.StatusBadRequest)
		return
	}
	var flow flowState
	if err := s.transient.Decode(TransientCookieName, cookie.Value, &flow); err != nil || flow.State == "" || flow.Nonce == "" || flow.Verifier == "" {
		s.clearTransient(w)
		http.Error(w, "invalid login session", http.StatusBadRequest)
		return
	}
	queryState := r.URL.Query().Get("state")
	if queryState == "" || subtle.ConstantTimeCompare([]byte(queryState), []byte(flow.State)) != 1 {
		s.clearTransient(w)
		http.Error(w, "invalid login state", http.StatusBadRequest)
		return
	}
	if providerErr := r.URL.Query().Get("error"); providerErr != "" {
		s.clearTransient(w)
		http.Error(w, "login was not completed", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		s.clearTransient(w)
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}
	token, err := s.oauth.Exchange(r.Context(), code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		s.clearTransient(w)
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		s.clearTransient(w)
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	idToken, err := s.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		s.clearTransient(w)
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	var claims struct {
		Subject           string `json:"sub"`
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Nonce             string `json:"nonce"`
	}
	if err := idToken.Claims(&claims); err != nil || claims.Subject == "" || claims.Nonce == "" || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(flow.Nonce)) != 1 {
		s.clearTransient(w)
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}
	user := User{Sub: claims.Subject, Email: claims.Email, Name: name, Role: "admin"}
	encoded, err := s.session.Encode(SessionCookieName, user)
	if err != nil {
		s.clearTransient(w)
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: encoded, Path: "/", MaxAge: int(s.ttl.Seconds()),
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode,
	})
	s.clearTransient(w)
	next := flow.Next
	if next == "" {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusFound)
}

// Logout clears the local session cookie.
func (s *Service) Logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

// SessionUser validates a session cookie and returns its user claims.
func (s *Service) SessionUser(r *http.Request) (User, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return User{}, errInvalidSession
	}
	var user User
	if err := s.session.Decode(SessionCookieName, cookie.Value, &user); err != nil || user.Sub == "" {
		return User{}, errInvalidSession
	}
	user.Role = "admin"
	return user, nil
}

func (s *Service) clearTransient(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: TransientCookieName, Value: "", Path: "/auth/", MaxAge: -1, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode})
}

// SanitizeNext permits only root-relative paths and rejects network-path references.
func SanitizeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n") {
		return "/"
	}
	parsed, err := url.ParseRequestURI(next)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return "/"
	}
	return next
}

func randomToken(n int) (string, error) {
	data := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, data); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func urlScheme(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Scheme
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// EncodeSession is intended for middleware tests and integrations that need a session cookie.
func (s *Service) EncodeSession(user User) (string, error) {
	user.Role = "admin"
	return s.session.Encode(SessionCookieName, user)
}
