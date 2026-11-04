package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeOIDCProvider struct {
	server   *httptest.Server
	clientID string
	key      *rsa.PrivateKey
}

func newFakeOIDCProvider(t *testing.T) *fakeOIDCProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	f := &fakeOIDCProvider{clientID: "test-client", key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                f.server.URL,
			"authorization_endpoint":                f.server.URL + "/authorize",
			"token_endpoint":                        f.server.URL + "/token",
			"jwks_uri":                              f.server.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key:       &key.PublicKey,
			KeyID:     "test-key",
			Algorithm: string(jose.RS256),
			Use:       "sig",
		}}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		redirectURI, err := url.Parse(query.Get("redirect_uri"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		redirectQuery := redirectURI.Query()
		redirectQuery.Set("code", query.Get("nonce"))
		redirectQuery.Set("state", query.Get("state"))
		redirectURI.RawQuery = redirectQuery.Encode()
		http.Redirect(w, r, redirectURI.String(), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		code := r.PostFormValue("code")
		if code == "invalid" {
			http.Error(w, "invalid_grant", http.StatusBadRequest)
			return
		}
		idToken, err := f.idToken(code)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"access_token": "access",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     idToken,
		})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeOIDCProvider) idToken(nonce string) (string, error) {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: f.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"),
	)
	if err != nil {
		return "", err
	}
	now := time.Now()
	payload, err := json.Marshal(map[string]any{
		"iss":   f.server.URL,
		"aud":   f.clientID,
		"sub":   "user",
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
		"nonce": nonce,
	})
	if err != nil {
		return "", err
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return signed.CompactSerialize()
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func testOIDCApp(t *testing.T, fake *fakeOIDCProvider) *app {
	t.Helper()
	app := testApp(t)
	provider, err := newOIDCProvider(context.Background(), oidcConfig{
		Issuer:       fake.server.URL,
		ClientID:     fake.clientID,
		ClientSecret: "secret",
		RedirectURL:  "http://example.com/oidc/callback",
	})
	require.NoError(t, err)
	app.oidc = provider
	app.config.ShortUrl = "http://example.com"
	app.config.DefaultUrl = "http://long.example.com"
	return app
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func oidcLoginCookies(t *testing.T, router http.Handler) []*http.Cookie {
	t.Helper()
	req := httptest.NewRequest("GET", "http://example.com/oidc/login", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusSeeOther, w.Result().StatusCode)
	return w.Result().Cookies()
}

func TestOIDCLoginFlow(t *testing.T) {
	fake := newFakeOIDCProvider(t)
	app := testOIDCApp(t, fake)
	defer closeTestApp(t, app)
	router := app.initRouter()

	req := httptest.NewRequest("GET", "http://example.com/l?sort=slug", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	resp := w.Result()
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loginPath := resp.Header.Get("Location")
	assert.Equal(t, "/oidc/login?redirect=%2Fl%3Fsort%3Dslug", loginPath)

	loginReq := httptest.NewRequest("GET", "http://example.com"+loginPath, nil)
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)
	loginResp := loginW.Result()
	require.Equal(t, http.StatusSeeOther, loginResp.StatusCode)

	authURL, err := url.Parse(loginResp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, fake.server.URL+"/authorize", authURL.Scheme+"://"+authURL.Host+authURL.Path)
	assert.NotEmpty(t, authURL.Query().Get("state"))
	assert.NotEmpty(t, authURL.Query().Get("nonce"))
	assert.Equal(t, fake.clientID, authURL.Query().Get("client_id"))
	assert.Equal(t, "code", authURL.Query().Get("response_type"))

	loginCookies := loginResp.Cookies()
	stateCookie := findCookie(loginCookies, oidcStateCookie)
	require.NotNil(t, stateCookie)
	assert.Equal(t, authURL.Query().Get("state"), stateCookie.Value)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authResp, err := client.Get(authURL.String())
	require.NoError(t, err)
	_ = authResp.Body.Close()
	require.Equal(t, http.StatusFound, authResp.StatusCode)
	callbackURL, err := url.Parse(authResp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, authURL.Query().Get("state"), callbackURL.Query().Get("state"))

	callbackReq := httptest.NewRequest("GET", "http://example.com"+callbackURL.RequestURI(), nil)
	for _, cookie := range loginCookies {
		callbackReq.AddCookie(cookie)
	}
	callbackW := httptest.NewRecorder()
	router.ServeHTTP(callbackW, callbackReq)
	callbackResp := callbackW.Result()
	require.Equal(t, http.StatusSeeOther, callbackResp.StatusCode)
	assert.Equal(t, "/l?sort=slug", callbackResp.Header.Get("Location"))

	sessionCookie := findCookie(callbackResp.Cookies(), sessionCookieName)
	require.NotNil(t, sessionCookie)
	assert.Greater(t, sessionCookie.MaxAge, 0)
	assert.True(t, sessionCookie.HttpOnly)

	listReq := httptest.NewRequest("GET", "http://example.com/l", nil)
	listReq.AddCookie(sessionCookie)
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)
	assert.Equal(t, http.StatusOK, listW.Result().StatusCode)
}

func TestOIDCSessionCookie(t *testing.T) {
	fake := newFakeOIDCProvider(t)
	app := testOIDCApp(t, fake)
	defer closeTestApp(t, app)

	t.Run("Valid session", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/l", nil)
		req.AddCookie(app.oidc.sessionCookie(time.Now()))
		assert.True(t, app.oidc.validSession(req))
	})
	t.Run("Expired session", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/l", nil)
		req.AddCookie(app.oidc.sessionCookie(time.Now().Add(-2 * sessionLifetime)))
		assert.False(t, app.oidc.validSession(req))
	})
	t.Run("Tampered session", func(t *testing.T) {
		cookie := app.oidc.sessionCookie(time.Now())
		expiry, signature, ok := strings.Cut(cookie.Value, ".")
		require.True(t, ok)
		first := byte('A')
		if signature[0] == 'A' {
			first = 'B'
		}
		cookie.Value = expiry + "." + string(first) + signature[1:]
		req := httptest.NewRequest("GET", "http://example.com/l", nil)
		req.AddCookie(cookie)
		assert.False(t, app.oidc.validSession(req))
	})
	t.Run("Malformed session", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/l", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "garbage"})
		assert.False(t, app.oidc.validSession(req))
	})
	t.Run("Missing session", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/l", nil)
		assert.False(t, app.oidc.validSession(req))
	})
}

func TestOIDCLoginMiddleware(t *testing.T) {
	fake := newFakeOIDCProvider(t)
	app := testOIDCApp(t, fake)
	app.config.Password = "abc"
	defer closeTestApp(t, app)

	handler := app.loginMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))

	t.Run("Redirects to OIDC login", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/l?sort=slug", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()
		assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
		assert.Equal(t, "/oidc/login?redirect=%2Fl%3Fsort%3Dslug", resp.Header.Get("Location"))
	})
	t.Run("Session grants access", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/l", nil)
		req.AddCookie(app.oidc.sessionCookie(time.Now()))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotModified, w.Result().StatusCode)
	})
	t.Run("Password still grants access", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/l", nil)
		req.SetBasicAuth("username", "abc")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotModified, w.Result().StatusCode)
	})
	t.Run("Wrong password is rejected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/l", nil)
		req.SetBasicAuth("username", "wrong")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.NotEmpty(t, resp.Header.Get("WWW-Authenticate"))
	})
	t.Run("Wrong password query is rejected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/l?password=wrong", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})
}

func TestOIDCCallbackErrors(t *testing.T) {
	fake := newFakeOIDCProvider(t)
	app := testOIDCApp(t, fake)
	defer closeTestApp(t, app)
	router := app.initRouter()

	call := func(t *testing.T, requestURL string, cookies []*http.Cookie) int {
		t.Helper()
		req := httptest.NewRequest("GET", requestURL, nil)
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Result().StatusCode
	}

	t.Run("Provider error", func(t *testing.T) {
		cookies := oidcLoginCookies(t, router)
		status := call(t, "http://example.com/oidc/callback?error=access_denied", cookies)
		assert.Equal(t, http.StatusBadRequest, status)
	})
	t.Run("Missing state cookie", func(t *testing.T) {
		status := call(t, "http://example.com/oidc/callback?state=abc&code=abc", nil)
		assert.Equal(t, http.StatusBadRequest, status)
	})
	t.Run("State mismatch", func(t *testing.T) {
		cookies := oidcLoginCookies(t, router)
		status := call(t, "http://example.com/oidc/callback?state=wrong&code=abc", cookies)
		assert.Equal(t, http.StatusBadRequest, status)
	})
	t.Run("Missing code", func(t *testing.T) {
		cookies := oidcLoginCookies(t, router)
		state := findCookie(cookies, oidcStateCookie)
		require.NotNil(t, state)
		status := call(t, "http://example.com/oidc/callback?state="+state.Value, cookies)
		assert.Equal(t, http.StatusBadRequest, status)
	})
	t.Run("Token exchange failure", func(t *testing.T) {
		cookies := oidcLoginCookies(t, router)
		state := findCookie(cookies, oidcStateCookie)
		require.NotNil(t, state)
		status := call(t, "http://example.com/oidc/callback?state="+state.Value+"&code=invalid", cookies)
		assert.Equal(t, http.StatusUnauthorized, status)
	})
	t.Run("Nonce mismatch", func(t *testing.T) {
		cookies := oidcLoginCookies(t, router)
		state := findCookie(cookies, oidcStateCookie)
		require.NotNil(t, state)
		status := call(t, "http://example.com/oidc/callback?state="+state.Value+"&code=different-nonce", cookies)
		assert.Equal(t, http.StatusBadRequest, status)
	})
}

func TestOIDCRedirectTarget(t *testing.T) {
	fake := newFakeOIDCProvider(t)
	app := testOIDCApp(t, fake)
	defer closeTestApp(t, app)
	router := app.initRouter()

	t.Run("Rejects external redirect at login", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/oidc/login?redirect=%2F%2Fevil.example", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp := w.Result()
		require.Equal(t, http.StatusSeeOther, resp.StatusCode)
		redirectCookie := findCookie(resp.Cookies(), oidcRedirectCookie)
		require.NotNil(t, redirectCookie)
		assert.Equal(t, "/", redirectCookie.Value)
	})

	t.Run("Rejects external redirect cookie at callback", func(t *testing.T) {
		cookies := oidcLoginCookies(t, router)
		state := findCookie(cookies, oidcStateCookie)
		nonce := findCookie(cookies, oidcNonceCookie)
		require.NotNil(t, state)
		require.NotNil(t, nonce)

		for _, target := range []string{"//evil.example", "/\\evil.example", "/\t/evil.example"} {
			cookiesWithTarget := append([]*http.Cookie{{Name: oidcRedirectCookie, Value: target}}, cookies...)
			req := httptest.NewRequest("GET", "http://example.com/oidc/callback?state="+state.Value+"&code="+nonce.Value, nil)
			for _, cookie := range cookiesWithTarget {
				req.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			resp := w.Result()
			require.Equal(t, http.StatusSeeOther, resp.StatusCode)
			location := resp.Header.Get("Location")
			assert.True(t, isLocalURL(location), location)
			assert.False(t, strings.HasPrefix(location, "//"), location)
			assert.NotContains(t, location, "://", location)
		}
	})
}

func TestOIDCDisabled(t *testing.T) {
	app := testApp(t)
	defer closeTestApp(t, app)

	router := app.initRouter()

	req := httptest.NewRequest("GET", "http://example.com/oidc/login", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Result().StatusCode)

	req = httptest.NewRequest("GET", "http://example.com/l", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, `Basic realm="Please enter a password!"`, resp.Header.Get("WWW-Authenticate"))
}

func TestIsLocalURL(t *testing.T) {
	assert.True(t, isLocalURL("/"))
	assert.True(t, isLocalURL("/l?sort=slug"))
	assert.False(t, isLocalURL(""))
	assert.False(t, isLocalURL("//evil.example"))
	assert.False(t, isLocalURL("https://evil.example"))
	assert.False(t, isLocalURL("/\\evil.example"))
	assert.False(t, isLocalURL("/\t/evil.example"))
	assert.False(t, isLocalURL("/\n/evil.example"))
	assert.False(t, isLocalURL("/\r/evil.example"))
}
