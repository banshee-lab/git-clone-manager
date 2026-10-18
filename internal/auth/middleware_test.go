package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequireSession(t *testing.T) {
	provider := newFakeProvider(t)
	service, _ := provider.service(t, "http://localhost:8080", 12*time.Hour)
	protected := service.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			t.Error("session user missing from request context")
			return
		}
		if user.Sub != "admin-1" || user.Role != "admin" {
			t.Errorf("context user = %+v", user)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	unauthenticated := httptest.NewRecorder()
	protected.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/test", nil))
	if unauthenticated.Code != http.StatusUnauthorized || !strings.Contains(unauthenticated.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("unauthenticated response = %d %q", unauthenticated.Code, unauthenticated.Body.String())
	}

	encoded, err := service.EncodeSession(User{Sub: "admin-1"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: encoded})
	authenticated := httptest.NewRecorder()
	protected.ServeHTTP(authenticated, request)
	if authenticated.Code != http.StatusNoContent {
		t.Fatalf("authenticated status = %d, want 204", authenticated.Code)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	provider := newFakeProvider(t)
	service, _ := provider.service(t, "https://app.example.test", 12*time.Hour)
	result := httptest.NewRecorder()
	service.Logout(result, httptest.NewRequest(http.MethodPost, "/auth/logout", nil))
	if result.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", result.Code)
	}
	cookies := result.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != SessionCookieName || cookies[0].MaxAge != -1 || cookies[0].Value != "" || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" {
		t.Fatalf("logout cookie = %+v", cookies)
	}
}

func TestCrossOriginProtection(t *testing.T) {
	if _, err := NewCSRFProtection("/relative"); err == nil {
		t.Fatal("relative base URL accepted")
	}
	protection, err := NewCSRFProtection("https://public.example.test/app")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/change", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /hooks/event", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	handler := protection.Handler(mux)

	tests := []struct {
		name   string
		path   string
		origin string
		status int
	}{
		{name: "cross-origin api blocked", path: "/api/change", origin: "https://attacker.example", status: http.StatusForbidden},
		{name: "trusted base origin allowed", path: "/api/change", origin: "https://public.example.test", status: http.StatusNoContent},
		{name: "hooks bypassed", path: "/hooks/event", origin: "https://attacker.example", status: http.StatusAccepted},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, nil)
			request.Header.Set("Origin", test.origin)
			if test.path == "/api/change" {
				request.Host = "internal.local"
			}
			result := httptest.NewRecorder()
			handler.ServeHTTP(result, request)
			if result.Code != test.status {
				t.Fatalf("status = %d, want %d (%s)", result.Code, test.status, result.Body.String())
			}
		})
	}
}
