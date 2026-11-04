package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	oidcStateCookie    = "goshort_oidc_state"
	oidcNonceCookie    = "goshort_oidc_nonce"
	oidcRedirectCookie = "goshort_oidc_redirect"
	sessionCookieName  = "goshort_session"
	oidcCookieLifetime = 10 * time.Minute
	sessionLifetime    = 7 * 24 * time.Hour
)

type oidcConfig struct {
	Issuer       string `mapstructure:"issuer"`
	ClientID     string `mapstructure:"clientId"`
	ClientSecret string `mapstructure:"clientSecret"`
	RedirectURL  string `mapstructure:"redirectUrl"`
}

type oidcProvider struct {
	verifier *oidc.IDTokenVerifier
	oauth2   *oauth2.Config
	secret   []byte
	secure   bool
}

func newOIDCProvider(ctx context.Context, cfg oidcConfig) (*oidcProvider, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.RedirectURL == "" {
		return nil, errors.New("oidc issuer, clientId and redirectUrl are required")
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return &oidcProvider{
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth2: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       []string{oidc.ScopeOpenID},
		},
		secret: secret,
		secure: strings.HasPrefix(cfg.RedirectURL, "https://"),
	}, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func isLocalURL(target string) bool {
	if target == "/" {
		return true
	}
	return len(target) > 1 &&
		target[0] == '/' &&
		target[1] != '/' &&
		target[1] != '\\' &&
		!strings.ContainsAny(target, "\t\r\n")
}

func (p *oidcProvider) sign(value string) []byte {
	mac := hmac.New(sha256.New, p.secret)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

func (p *oidcProvider) sessionValue(now time.Time) string {
	expiry := strconv.FormatInt(now.Add(sessionLifetime).Unix(), 10)
	return expiry + "." + base64.RawURLEncoding.EncodeToString(p.sign(expiry))
}

func (p *oidcProvider) validSession(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	expiry, sig, ok := strings.Cut(cookie.Value, ".")
	if !ok {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(decoded, p.sign(expiry)) {
		return false
	}
	expiryTime, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < expiryTime
}

func (p *oidcProvider) temporaryCookie(name, value string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(oidcCookieLifetime.Seconds()),
		HttpOnly: true,
		Secure:   p.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (p *oidcProvider) sessionCookie(now time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    p.sessionValue(now),
		Path:     "/",
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   p.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (p *oidcProvider) clearCookie(name string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   p.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (p *oidcProvider) clearTemporaryCookies(w http.ResponseWriter) {
	for _, name := range []string{oidcStateCookie, oidcNonceCookie, oidcRedirectCookie} {
		http.SetCookie(w, p.clearCookie(name))
	}
}

func (a *app) oidcLoginHandler(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		http.Error(w, "Failed to start login", http.StatusInternalServerError)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		http.Error(w, "Failed to start login", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.oidc.temporaryCookie(oidcStateCookie, state))
	http.SetCookie(w, a.oidc.temporaryCookie(oidcNonceCookie, nonce))
	returnTo := "/"
	if target := r.URL.Query().Get("redirect"); isLocalURL(target) {
		returnTo = target
	}
	http.SetCookie(w, a.oidc.temporaryCookie(oidcRedirectCookie, returnTo))
	http.Redirect(w, r, a.oidc.oauth2.AuthCodeURL(state, oidc.Nonce(nonce)), http.StatusSeeOther)
}

func (a *app) oidcCallbackHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("error") != "" {
		http.Error(w, "Authentication failed: "+query.Get("error"), http.StatusBadRequest)
		return
	}
	stateCookie, err := r.Cookie(oidcStateCookie)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != query.Get("state") {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}
	nonceCookie, err := r.Cookie(oidcNonceCookie)
	if err != nil || nonceCookie.Value == "" {
		http.Error(w, "Missing nonce", http.StatusBadRequest)
		return
	}
	if query.Get("code") == "" {
		http.Error(w, "Missing code", http.StatusBadRequest)
		return
	}

	token, err := a.oidc.oauth2.Exchange(r.Context(), query.Get("code"))
	if err != nil {
		log.Println("OIDC token exchange failed:", err.Error())
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	}
	idToken, err := a.oidc.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		log.Println("OIDC ID token verification failed:", err.Error())
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	}
	var claims struct {
		Nonce string `json:"nonce"`
	}
	if err := idToken.Claims(&claims); err != nil || claims.Nonce != nonceCookie.Value {
		http.Error(w, "Invalid nonce", http.StatusBadRequest)
		return
	}

	returnTo := "/"
	if redirectCookie, err := r.Cookie(oidcRedirectCookie); err == nil {
		if isLocalURL(redirectCookie.Value) {
			returnTo = redirectCookie.Value
		}
	}

	a.oidc.clearTemporaryCookies(w)
	http.SetCookie(w, a.oidc.sessionCookie(time.Now()))
	http.Redirect(w, r, returnTo, http.StatusSeeOther)
}

func (a *app) oidcLoginURL(returnTo string) string {
	return "/oidc/login?redirect=" + url.QueryEscape(returnTo)
}
