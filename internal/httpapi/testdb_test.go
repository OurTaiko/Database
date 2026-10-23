package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"ourtaiko.dev/database/api/internal/database"
)

// testDB gives each test its own migrated database on the server named by
// DATABASE_TEST_URL (a role that may create databases), dropped afterwards.
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	admin := os.Getenv("DATABASE_TEST_URL")
	if admin == "" {
		t.Skip("set DATABASE_TEST_URL to test against PostgreSQL")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("api_test_%d", time.Now().UnixNano())
	if _, err = conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		conn.Close(ctx)
		t.Fatal(err)
	}
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	pool, err := database.Open(ctx, u.String())
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		conn.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		conn.Close(ctx)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

type testUser struct {
	ID, Nickname string
	Admin        bool
}

// testUsers are the accounts the fake account center knows; nickname = key.
var testUsers = map[string]testUser{
	"owner": {"d46774d30dd13b92d9e536808da468a4", "owner", false},
	"alice": {"9b893bc6d9422c93536ff0df503b81e9", "alice", false},
	"bob":   {"5f0c4a0b0e7b4d3c9a6f1e2d3c4b5a69", "bob", false},
	"admin": {"0a1b2c3d4e5f60718293a4b5c6d7e8f9", "admin", true},
}

func remoteUser(u testUser) map[string]any {
	return map[string]any{"id": u.ID, "username": u.Nickname, "nickname": u.Nickname, "isAdmin": u.Admin, "emailVerified": true}
}

// fakeSSO answers the internal account-center calls the server makes: a web
// access token "tok:<user id>" introspects to that user.
func fakeSSO(t *testing.T) *SSOClient {
	t.Helper()
	byID := map[string]testUser{}
	for _, u := range testUsers {
		byID[u.ID] = u
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/v1/web/introspect":
			token, _ := body["accessToken"].(string)
			u, ok := byID[strings.TrimPrefix(token, "tok:")]
			if !ok {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"user": remoteUser(u)})
		case "/internal/v1/users/lookup":
			users := []map[string]any{}
			ids, _ := body["userIds"].([]any)
			for _, id := range ids {
				if u, ok := byID[id.(string)]; ok {
					users = append(users, remoteUser(u))
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"users": users})
		case "/internal/v1/users/search":
			q, _ := body["query"].(string)
			ids := []string{}
			for _, u := range byID {
				if strings.Contains(u.Nickname, q) {
					ids = append(ids, u.ID)
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"userIds": ids, "next": ""})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(remote.Close)
	sso, err := NewSSO(SSOConfig{Issuer: remote.URL, RedirectURL: "http://localhost/api/v1/auth/sso/callback", EncryptionKey: strings.Repeat("ab", 32),
		ServiceID: "database", ServiceKey: strings.Repeat("k", 40), ClientID: "database", ClientSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	return sso
}

type testClient struct {
	t       *testing.T
	handler http.Handler
	tokens  map[string]string
	n       *atomic.Int64
}

// newTestClient signs every test user in with a website session.
func newTestClient(t *testing.T, s *Server) testClient {
	t.Helper()
	ctx := context.Background()
	c := testClient{t, s.Handler(), map[string]string{}, &atomic.Int64{}}
	for name, u := range testUsers {
		token := ID() + ID()
		_, err := s.DB.Exec(ctx, `INSERT INTO users(id) VALUES($1) ON CONFLICT DO NOTHING`, u.ID)
		if err == nil {
			_, err = s.DB.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at,upstream_token) VALUES($1,$2,'csrf',now()+interval '1 day',$3)`,
				hash(token), u.ID, s.Config.SSO.seal("tok:"+u.ID, hash(token)))
		}
		if err != nil {
			t.Fatal(err)
		}
		c.tokens[name] = token
	}
	return c
}

// do sends a request as the given user ("" for a guest) and decodes the JSON reply.
func (c testClient) do(user, method, path, body string, out any) int {
	c.t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	// A fresh client address per request keeps the per-address write limit out of the way.
	r.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1234", c.n.Load()>>16&255, c.n.Load()>>8&255, c.n.Add(1)&255)
	if method != "GET" {
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-CSRF-Token", "csrf")
	}
	if user != "" {
		r.AddCookie(&http.Cookie{Name: "ourtaiko_db_session", Value: c.tokens[user]})
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if out != nil && w.Code < 300 {
		if e := json.Unmarshal(w.Body.Bytes(), out); e != nil {
			c.t.Fatal(path, e, w.Body.String())
		}
	}
	return w.Code
}

// raw sends a GET as the given user and returns the status and body unparsed.
func (c testClient) raw(user, path string) (int, []byte) {
	c.t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	if user != "" {
		r.AddCookie(&http.Cookie{Name: "ourtaiko_db_session", Value: c.tokens[user]})
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}
