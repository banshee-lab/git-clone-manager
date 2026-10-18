package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"

	"gitsync/internal/config"
)

type fakeProvider struct {
	mu           sync.Mutex
	server       *httptest.Server
	key          *rsa.PrivateKey
	challenge    string
	codeVerifier string
	claims       map[string]any
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeProvider{key: key, claims: map[string]any{}}
	mux := http.NewServeMux()
	f.server = httptest.NewTLSServer(mux)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer": f.server.URL, "authorization_endpoint": f.server.URL + "/authorize",
			"token_endpoint": f.server.URL + "/token", "jwks_uri": f.server.URL + "/keys",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &f.key.PublicKey, KeyID: "test-key", Algorithm: string(jose.RS256), Use: "sig"}}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.challenge = r.URL.Query().Get("code_challenge")
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.codeVerifier = r.Form.Get("code_verifier")
		challenge := f.challenge
		claims := make(map[string]any, len(f.claims))
		for key, value := range f.claims {
			claims[key] = value
		}
		f.mu.Unlock()
		challengeSum := sha256.Sum256([]byte(f.codeVerifier))
		if base64.RawURLEncoding.EncodeToString(challengeSum[:]) != challenge {
			http.Error(w, "bad pkce", http.StatusBadRequest)
			return
		}
		if _, ok := claims["iss"]; !ok {
			claims["iss"] = f.server.URL
		}
		if _, ok := claims["aud"]; !ok {
			claims["aud"] = "test-client"
		}
		if _, ok := claims["exp"]; !ok {
			claims["exp"] = time.Now().Add(5 * time.Minute).Unix()
		}
		if _, ok := claims["iat"]; !ok {
			claims["iat"] = time.Now().Unix()
		}
		jwt, err := f.sign(claims)
		if err != nil {
			http.Error(w, "sign failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"access_token": "access", "token_type": "Bearer", "expires_in": 300, "id_token": jwt})
	})
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeProvider) sign(claims map[string]any) (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"))
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signed, err := signer.Sign(data)
	if err != nil {
		return "", err
	}
	return signed.CompactSerialize()
}

func (f *fakeProvider) authorize(t *testing.T, authorizeURL *url.URL) {
	t.Helper()
	response, err := f.server.Client().Get(f.server.URL + "/authorize?" + authorizeURL.RawQuery)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fake authorization endpoint returned %d", response.StatusCode)
	}
}

func (f *fakeProvider) service(t *testing.T, baseURL string, ttl time.Duration) (*Service, context.Context) {
	t.Helper()
	cfg := config.Default()
	cfg.Server.BaseURL = baseURL
	cfg.OIDC.IssuerURL = f.server.URL
	cfg.OIDC.ClientID = "test-client"
	cfg.OIDC.ClientSecret = "test-secret"
	cfg.OIDC.RedirectURL = "https://app.example.test/auth/callback"
	cfg.OIDC.SessionSecret = strings.Repeat("s", 32)
	cfg.OIDC.SessionTTL = config.Duration(ttl)
	ctx := oidc.ClientContext(context.Background(), f.server.Client())
	service, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return service, ctx
}

func TestService_LoginCallbackRoundTrip(t *testing.T) {
	provider := newFakeProvider(t)
	provider.claims = map[string]any{
		"sub": "user-123", "email": "admin@example.test", "name": "Admin",
		"nonce": "", // filled from the authorization request below
	}
	service, ctx := provider.service(t, "https://app.example.test", time.Hour)

	login := httptest.NewRequest(http.MethodGet, "/auth/login?next=%2Frepositories%3Fx%3D1", nil)
	loginResult := httptest.NewRecorder()
	service.Login(loginResult, login)
	if loginResult.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302", loginResult.Code)
	}
	authorizeURL, err := url.Parse(loginResult.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if authorizeURL.Query().Get("response_type") != "code" || authorizeURL.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization request missing code/PKCE: %s", authorizeURL)
	}
	provider.authorize(t, authorizeURL)
	transient := loginResult.Result().Cookies()[0]
	if transient.Name != TransientCookieName || !transient.HttpOnly || !transient.Secure || transient.SameSite != http.SameSiteLaxMode || transient.Path != "/auth/" {
		t.Fatalf("transient cookie flags = %+v", transient)
	}
	provider.mu.Lock()
	provider.claims["nonce"] = authorizeURL.Query().Get("nonce")
	provider.mu.Unlock()
	callback := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+url.QueryEscape(authorizeURL.Query().Get("state"))+"&code=valid", nil).WithContext(ctx)
	callback.AddCookie(transient)
	callbackResult := httptest.NewRecorder()
	service.Callback(callbackResult, callback)
	if callbackResult.Code != http.StatusFound || callbackResult.Header().Get("Location") != "/repositories?x=1" {
		provider.mu.Lock()
		challenge, verifier := provider.challenge, provider.codeVerifier
		provider.mu.Unlock()
		t.Fatalf("callback response = %d %q, want redirect /repositories?x=1 (pkce challenge %q, verifier %q)", callbackResult.Code, callbackResult.Header().Get("Location"), challenge, verifier)
	}
	cookies := callbackResult.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("callback set %d cookies, want session and transient clear", len(cookies))
	}
	var session *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == SessionCookieName {
			session = cookie
		}
		if cookie.Name == TransientCookieName && cookie.MaxAge != -1 {
			t.Fatalf("transient cookie was not cleared: %+v", cookie)
		}
	}
	if session == nil || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode || session.Path != "/" || session.MaxAge != 3600 {
		t.Fatalf("session cookie flags = %+v", session)
	}
	provider.mu.Lock()
	verifier := provider.codeVerifier
	provider.mu.Unlock()
	if verifier == "" {
		t.Fatal("token endpoint did not receive a PKCE verifier")
	}
	request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	request.AddCookie(session)
	user, err := service.SessionUser(request)
	if err != nil || user.Sub != "user-123" || user.Email != "admin@example.test" || user.Name != "Admin" || user.Role != "admin" {
		t.Fatalf("session user = %+v, err %v", user, err)
	}
}

func TestLoginTransientCookieSecurityDependsOnBaseURL(t *testing.T) {
	for _, test := range []struct {
		name       string
		baseURL    string
		wantSecure bool
	}{{"https", "https://app.example.test", true}, {"http", "http://localhost:8080", false}} {
		t.Run(test.name, func(t *testing.T) {
			provider := newFakeProvider(t)
			service, _ := provider.service(t, test.baseURL, time.Hour)
			result := httptest.NewRecorder()
			service.Login(result, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
			if got := result.Result().Cookies()[0].Secure; got != test.wantSecure {
				t.Fatalf("Secure = %v, want %v", got, test.wantSecure)
			}
		})
	}
}

func TestCallbackRejectsInvalidStateAndTransientCookie(t *testing.T) {
	provider := newFakeProvider(t)
	service, _ := provider.service(t, "http://localhost", time.Hour)
	loginResult := httptest.NewRecorder()
	service.Login(loginResult, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	transient := loginResult.Result().Cookies()[0]
	for _, test := range []struct {
		name   string
		cookie *http.Cookie
		state  string
	}{{"missing transient", nil, "wrong"}, {"tampered transient", &http.Cookie{Name: TransientCookieName, Value: transient.Value + "x"}, "wrong"}, {"wrong state", transient, "wrong"}} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+test.state+"&code=valid", nil)
			if test.cookie != nil {
				request.AddCookie(test.cookie)
			}
			result := httptest.NewRecorder()
			service.Callback(result, request)
			if result.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", result.Code)
			}
		})
	}
}

func TestCallbackRejectsInvalidIDTokenClaims(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{{"bad nonce", func(claims map[string]any) { claims["nonce"] = "wrong" }}, {"expired token", func(claims map[string]any) { claims["exp"] = time.Now().Add(-time.Minute).Unix() }}, {"wrong audience", func(claims map[string]any) { claims["aud"] = "other-client" }}, {"wrong issuer", func(claims map[string]any) { claims["iss"] = "https://wrong.example.test" }}} {
		t.Run(test.name, func(t *testing.T) {
			provider := newFakeProvider(t)
			service, ctx := provider.service(t, "http://localhost", time.Hour)
			loginResult := httptest.NewRecorder()
			service.Login(loginResult, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
			cookies := loginResult.Result().Cookies()
			authURL, _ := url.Parse(loginResult.Header().Get("Location"))
			provider.authorize(t, authURL)
			claims := map[string]any{"sub": "subject", "nonce": authURL.Query().Get("nonce")}
			test.mutate(claims)
			provider.mu.Lock()
			provider.claims = claims
			provider.mu.Unlock()
			request := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+url.QueryEscape(authURL.Query().Get("state"))+"&code=valid", nil).WithContext(ctx)
			request.AddCookie(cookies[0])
			result := httptest.NewRecorder()
			service.Callback(result, request)
			if result.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", result.Code)
			}
		})
	}
}

func TestSessionTamperedAndExpired(t *testing.T) {
	provider := newFakeProvider(t)
	service, _ := provider.service(t, "http://localhost", time.Second)
	encoded, err := service.EncodeSession(User{Sub: "subject"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: encoded + "x"})
	if _, err := service.SessionUser(request); err == nil {
		t.Fatal("tampered cookie accepted")
	}
	time.Sleep(2100 * time.Millisecond)
	request = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: encoded})
	if _, err := service.SessionUser(request); err == nil {
		t.Fatal("expired cookie accepted")
	}
}

func TestDeriveCookieKeys(t *testing.T) {
	hash1, block1, err := DeriveCookieKeys(strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	hash2, block2, err := DeriveCookieKeys(strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	if string(hash1) != string(hash2) || string(block1) != string(block2) {
		t.Fatal("key derivation is not deterministic")
	}
	if string(hash1) == string(block1) {
		t.Fatal("signing and encryption keys are not distinct")
	}
	if _, _, err := DeriveCookieKeys("short"); err == nil {
		t.Fatal("short session secret accepted")
	}
}

func TestSanitizeNext(t *testing.T) {
	for _, test := range []struct{ value, want string }{
		{"//evil.com", "/"}, {"https://evil.com", "/"}, {"/ok?x=1", "/ok?x=1"}, {"/\\evil.com", "/"}, {"/\r\nLocation:evil", "/"}, {"", "/"},
	} {
		t.Run(fmt.Sprintf("%q", test.value), func(t *testing.T) {
			if got := SanitizeNext(test.value); got != test.want {
				t.Fatalf("SanitizeNext(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		panic(err)
	}
}
