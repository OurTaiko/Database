package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/oauth2"
)

type User struct {
	PreferredLanguage string `json:"preferredLanguage"`
	ID                string `json:"id"`
	Username          string `json:"username"`
	Nickname          string `json:"nickname"`
	AvatarURL         string `json:"avatarUrl"`
	EmailVerified     bool   `json:"emailVerified"`
	// Administrators upload charts; owners manage their own.
	IsAdmin bool `json:"isAdmin"`
}
type session struct {
	User          User
	CSRF          string
	Token         string
	RefreshToken  string
	AccessExpires time.Time
}

func (s *Server) localSession(r *http.Request) (session, error) {
	var v session
	c, e := r.Cookie("ourtaiko_db_session")
	if e != nil || len(c.Value) != 64 {
		return v, pgx.ErrNoRows
	}
	var encrypted, refresh []byte
	var accessExpires *time.Time
	e = s.DB.QueryRow(r.Context(), `SELECT user_id,csrf_token,upstream_token,upstream_refresh_token,upstream_expires_at FROM sessions WHERE token_hash=$1 AND expires_at>now()`, hash(c.Value)).Scan(&v.User.ID, &v.CSRF, &encrypted, &refresh, &accessExpires)
	if e != nil {
		return v, e
	}
	if s.Config.SSO == nil {
		// Development sessions carry no account-center credential.
		return v, nil
	}
	v.Token, e = s.Config.SSO.unseal(encrypted, hash(c.Value))
	if e == nil && len(refresh) > 0 {
		v.RefreshToken, e = s.Config.SSO.unseal(refresh, "refresh:"+hash(c.Value))
	}
	if accessExpires != nil {
		v.AccessExpires = *accessExpires
	}
	return v, e
}

type activityWriter interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// Called only after authentication. Limit activity writes to once a minute.
func recordUserActivity(ctx context.Context, db activityWriter, id string, login bool) error {
	_, err := db.Exec(ctx, `INSERT INTO users(id,first_login_at,last_active_at)
 VALUES($1,CASE WHEN $2 THEN now() ELSE NULL END,now())
 ON CONFLICT(id) DO UPDATE SET
 first_login_at=COALESCE(users.first_login_at,EXCLUDED.first_login_at),
 last_active_at=GREATEST(users.last_active_at,EXCLUDED.last_active_at)
 WHERE users.last_active_at IS NULL OR users.last_active_at < now()-interval '1 minute'
 OR (users.first_login_at IS NULL AND EXCLUDED.first_login_at IS NOT NULL)`, id, login)
	return err
}

// devUser is the only account of a server running without an account center.
func devUser(id string) User {
	return User{ID: id, Username: "dev", Nickname: "本地开发用户", EmailVerified: true, IsAdmin: true}
}

func (s *Server) current(r *http.Request) (session, error) {
	if isGameRequest(r) {
		return s.gameSession(r)
	}
	v, e := s.localSession(r)
	if e != nil {
		return v, e
	}
	if s.Config.SSO == nil {
		if s.Config.DevUserID == "" || v.User.ID != s.Config.DevUserID {
			return session{}, pgx.ErrNoRows
		}
		v.User = devUser(v.User.ID)
	} else {
		if v.RefreshToken != "" && time.Until(v.AccessExpires) < time.Minute {
			v, e = s.refreshWebSession(r)
			if e != nil {
				return session{}, e
			}
		}
		u, e := s.Config.SSO.identity(r.Context(), "web", v.Token)
		if e != nil {
			return session{}, e
		}
		if u.ID != v.User.ID {
			return session{}, pgx.ErrNoRows
		}
		v.User = u
	}
	if e = recordUserActivity(r.Context(), s.DB, v.User.ID, false); e != nil {
		return session{}, e
	}
	return v, nil
}

// gameSession authenticates a native client by its SSO game token. Browser
// cookies never make a game session, nor a game token a website session.
func (s *Server) gameSession(r *http.Request) (session, error) {
	var v session
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || len(token) != 64 {
		return v, pgx.ErrNoRows
	}
	if s.Config.SSO == nil {
		if s.Config.DevUserID == "" || token != s.devGameToken() {
			return v, pgx.ErrNoRows
		}
		v.User = devUser(s.Config.DevUserID)
	} else {
		u, e := s.Config.SSO.identity(r.Context(), "game", token)
		if e != nil {
			return v, e
		}
		v.User = u
	}
	return v, recordUserActivity(r.Context(), s.DB, v.User.ID, false)
}
func (s *Server) required(w http.ResponseWriter, r *http.Request, csrf bool) (session, bool) {
	v, e := s.current(r)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			problem(w, 401, "UNAUTHORIZED", "请先登录")
		} else {
			internal(w, e)
		}
		return v, false
	}
	if csrf && r.Header.Get("X-CSRF-Token") != v.CSRF {
		problem(w, 403, "CSRF_INVALID", "会话已更新，请刷新页面后重试")
		return v, false
	}
	return v, true
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	v, e := s.current(r)
	if errors.Is(e, pgx.ErrNoRows) {
		respond(w, 200, map[string]any{"user": nil, "csrfToken": ""})
		return
	}
	if e != nil {
		internal(w, e)
		return
	}
	respond(w, 200, map[string]any{"user": v.User, "csrfToken": v.CSRF})
}
func (s *Server) accountRedirect(w http.ResponseWriter, r *http.Request) {
	if s.Config.SSO == nil {
		problem(w, 404, "SSO_NOT_CONFIGURED", "此服务器未接入账号中心")
		return
	}
	path := "/"
	if r.PathValue("page") == "register" {
		path = "/register/"
	}
	http.Redirect(w, r, s.Config.SSO.config.Issuer+path, http.StatusSeeOther)
}
func safeReturn(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\r\n") || u.IsAbs() || u.Host != "" {
		return "/"
	}
	return raw
}

func (s *Server) setSession(w http.ResponseWriter, raw string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: "ourtaiko_db_session", Value: raw, Path: "/", HttpOnly: true, Secure: s.Config.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: max(1, int(time.Until(expires).Seconds()))})
}

// devLogin opens a session for the configured development user.
func (s *Server) devLogin(w http.ResponseWriter, r *http.Request, destination string) {
	ctx := r.Context()
	if e := recordUserActivity(ctx, s.DB, s.Config.DevUserID, true); e != nil {
		internal(w, e)
		return
	}
	rawSession, csrf := ID()+ID(), ID()+ID()
	expires := time.Now().Add(7 * 24 * time.Hour)
	if _, e := s.DB.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at,upstream_token) VALUES($1,$2,$3,$4,''::bytea)`, hash(rawSession), s.Config.DevUserID, csrf, expires); e != nil {
		internal(w, e)
		return
	}
	s.setSession(w, rawSession, expires)
	http.Redirect(w, r, s.Config.Origin+destination, http.StatusSeeOther)
}
func (s *Server) ssoLogin(w http.ResponseWriter, r *http.Request) {
	destination := safeReturn(r.URL.Query().Get("returnTo"))
	if strings.HasPrefix(destination, "/api/") {
		problem(w, 400, "RETURN_PATH_INVALID", "返回地址必须是网站页面")
		return
	}
	if s.Config.SSO == nil {
		if s.Config.DevUserID != "" {
			s.devLogin(w, r, destination)
			return
		}
		http.Redirect(w, r, s.Config.Origin+"/login?error=sso", http.StatusSeeOther)
		return
	}
	_, cfg, e := s.Config.SSO.oidc(r.Context())
	if e != nil {
		internal(w, e)
		return
	}
	state, binding, nonce, verifier := ID()+ID(), ID()+ID(), ID()+ID(), oauth2.GenerateVerifier()
	_, e = s.DB.Exec(r.Context(), `DELETE FROM oidc_flows WHERE expires_at<now()`)
	if e != nil {
		internal(w, e)
		return
	}
	_, e = s.DB.Exec(r.Context(), `INSERT INTO oidc_flows(state_hash,binding_hash,nonce,verifier,return_path,expires_at) VALUES($1,$2,$3,$4,$5,now()+interval '5 minutes')`, hash(state), hash(binding), nonce, verifier, destination)
	if e != nil {
		internal(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "ourtaiko_db_oidc", Value: binding, Path: "/api/v1/auth/sso", HttpOnly: true, Secure: s.Config.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, cfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusSeeOther)
}
func (s *Server) ssoCallback(w http.ResponseWriter, r *http.Request) {
	fail := func() { http.Redirect(w, r, s.Config.Origin+"/login?error=sso", http.StatusSeeOther) }
	if s.Config.SSO == nil {
		fail()
		return
	}
	c, e := r.Cookie("ourtaiko_db_oidc")
	state := r.URL.Query().Get("state")
	if e != nil || len(c.Value) != 64 || len(state) != 64 {
		fail()
		return
	}
	var nonce, verifier, destination string
	e = s.DB.QueryRow(r.Context(), `DELETE FROM oidc_flows WHERE state_hash=$1 AND binding_hash=$2 AND expires_at>now() RETURNING nonce,verifier,return_path`, hash(state), hash(c.Value)).Scan(&nonce, &verifier, &destination)
	http.SetCookie(w, &http.Cookie{Name: "ourtaiko_db_oidc", Path: "/api/v1/auth/sso", HttpOnly: true, Secure: s.Config.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if e != nil || r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		fail()
		return
	}
	provider, cfg, e := s.Config.SSO.oidc(r.Context())
	if e != nil {
		fail()
		return
	}
	ctx := oidc.ClientContext(r.Context(), s.Config.SSO.http)
	token, e := cfg.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
	if e != nil {
		fail()
		return
	}
	revoke := func() {
		_ = s.Config.SSO.call(ctx, "web/revoke", map[string]string{"accessToken": token.AccessToken, "refreshToken": token.RefreshToken}, nil)
	}
	raw, _ := token.Extra("id_token").(string)
	idToken, e := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}).Verify(ctx, raw)
	if e != nil || idToken.Nonce != nonce || !validSubject(idToken.Subject) {
		revoke()
		fail()
		return
	}
	if idToken.AccessTokenHash != "" && idToken.VerifyAccessToken(token.AccessToken) != nil {
		revoke()
		fail()
		return
	}
	u, e := s.Config.SSO.identity(ctx, "web", token.AccessToken)
	if e != nil || u.ID != idToken.Subject {
		revoke()
		fail()
		return
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		revoke()
		fail()
		return
	}
	defer tx.Rollback(ctx)
	if e = recordUserActivity(ctx, tx, u.ID, true); e != nil {
		revoke()
		fail()
		return
	}
	rawSession, csrf := ID()+ID(), ID()+ID()
	digest := hash(rawSession)
	expires := time.Now().Add(7 * 24 * time.Hour)
	if token.RefreshToken == "" || token.Expiry.IsZero() || !token.Expiry.After(time.Now()) {
		revoke()
		fail()
		return
	}
	_, e = tx.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at,upstream_token,upstream_refresh_token,upstream_expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, digest, u.ID, csrf, expires, s.Config.SSO.seal(token.AccessToken, digest), s.Config.SSO.seal(token.RefreshToken, "refresh:"+digest), token.Expiry)
	if e != nil {
		revoke()
		fail()
		return
	}
	if e = tx.Commit(ctx); e != nil {
		revoke()
		fail()
		return
	}
	if old, e := r.Cookie("ourtaiko_db_session"); e == nil {
		_, _ = s.DB.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, hash(old.Value))
	}
	s.setSession(w, rawSession, expires)
	http.Redirect(w, r, s.Config.Origin+safeReturn(destination), http.StatusSeeOther)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	// Local logout still works when SSO is unavailable. Never require a remote refresh first.
	v, e := s.localSession(r)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		internal(w, e)
		return
	}
	if e == nil {
		if r.Header.Get("X-CSRF-Token") != v.CSRF {
			problem(w, 403, "CSRF_INVALID", "会话已更新，请刷新页面后重试")
			return
		}
		c, _ := r.Cookie("ourtaiko_db_session")
		var access, refresh []byte
		e = s.DB.QueryRow(r.Context(), `DELETE FROM sessions WHERE token_hash=$1 RETURNING upstream_token,upstream_refresh_token`, hash(c.Value)).Scan(&access, &refresh)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			internal(w, e)
			return
		}
		if e == nil && s.Config.SSO != nil {
			v.Token, _ = s.Config.SSO.unseal(access, hash(c.Value))
			v.RefreshToken = ""
			if len(refresh) > 0 {
				v.RefreshToken, _ = s.Config.SSO.unseal(refresh, "refresh:"+hash(c.Value))
			}
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
			defer cancel()
			_ = s.Config.SSO.call(ctx, "web/revoke", map[string]string{"accessToken": v.Token, "refreshToken": v.RefreshToken}, nil)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "ourtaiko_db_session", Path: "/", HttpOnly: true, Secure: s.Config.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	respond(w, 200, map[string]bool{"ok": true})
}
